// Copyright 2017 Capsule8, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sensor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"

	telemetryAPI "github.com/Happyholic1203/capsule8/api/v0"
	"github.com/Happyholic1203/capsule8/pkg/config"
	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/golang/glog"

	"golang.org/x/sys/unix"

	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/genproto/googleapis/rpc/status"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TelemetryServiceGetEventsRequestFunc is a function called when a new
// subscription is requested.
type TelemetryServiceGetEventsRequestFunc func(
	request *telemetryAPI.GetEventsRequest,
)

// TelemetryServiceGetEventsResponseFunc is a function called when a new
// subscscription is processed. The response will be included or an error if
// there was an error processing the subscription.
type TelemetryServiceGetEventsResponseFunc func(
	response *telemetryAPI.GetEventsResponse,
	err error,
)

// TelemetryServiceStartFunc is a function called when the sensor service is
// started.
type TelemetryServiceStartFunc func()

// TelemetryServiceStopFunc is a function called when the sensor service is
// stopped.
type TelemetryServiceStopFunc func()

type telemetryServiceOptions struct {
	start             TelemetryServiceStartFunc
	stop              TelemetryServiceStopFunc
	getEventsRequest  TelemetryServiceGetEventsRequestFunc
	getEventsResponse TelemetryServiceGetEventsResponseFunc
}

// TelemetryServiceOption is used to implement optional arguments for
// NewTelemetryService. It must be exported, but it is not typically used
// directly.
type TelemetryServiceOption func(*telemetryServiceOptions)

// WithStartFunc specifies a function to be called when a telemetry service is
// started.
func WithStartFunc(f TelemetryServiceStartFunc) TelemetryServiceOption {
	return func(o *telemetryServiceOptions) {
		o.start = f
	}
}

// WithStopFunc specifies a function to be called when a telemetry service is
// stopped.
func WithStopFunc(f TelemetryServiceStopFunc) TelemetryServiceOption {
	return func(o *telemetryServiceOptions) {
		o.stop = f
	}
}

// WithGetEventsRequestFunc specifies a function to be called when a telemetry
// service GetEvents request has been received. It is called with the request.
func WithGetEventsRequestFunc(f TelemetryServiceGetEventsRequestFunc) TelemetryServiceOption {
	return func(o *telemetryServiceOptions) {
		o.getEventsRequest = f
	}
}

// WithGetEventsResponseFunc sepecifies a function to be called when a telemtry
// service GetEvents request has been processed. It is called with either the
// response or an error.
func WithGetEventsResponseFunc(f TelemetryServiceGetEventsResponseFunc) TelemetryServiceOption {
	return func(o *telemetryServiceOptions) {
		o.getEventsResponse = f
	}
}

// TelemetryService is a service that can be used with the ServiceManager to
// process telemetry subscription requests and stream the resulting telemetry
// events.
type TelemetryService struct {
	server *grpc.Server
	sensor *Sensor

	address string

	options telemetryServiceOptions
}

// NewTelemetryService creates a new TelemetryService instance that can be used
// with a ServiceManager instance.
func NewTelemetryService(
	sensor *Sensor,
	address string,
	options ...TelemetryServiceOption,
) *TelemetryService {
	ts := &TelemetryService{
		sensor:  sensor,
		address: address,
	}

	for _, o := range options {
		o(&ts.options)
	}

	return ts
}

// Name returns the human-readable name of the TelemetryService.
func (ts *TelemetryService) Name() string {
	return "gRPC Telemetry Server"
}

// Serve is the main entrypoint for the TelemetryService. It is normally called
// by the ServiceManager. It will service requests indefinitely from the calling
// Goroutine.
func (ts *TelemetryService) Serve() error {
	var (
		err error
		lis net.Listener
	)

	glog.V(1).Info("Serving gRPC API on ", ts.address)

	parts := strings.Split(ts.address, ":")
	if len(parts) > 1 && parts[0] == "unix" {
		socketPath := parts[1]

		// Check whether socket already exists and if someone
		// is already listening on it.
		_, err = os.Stat(socketPath)
		if err == nil {
			var ua *net.UnixAddr

			ua, err = net.ResolveUnixAddr("unix", socketPath)
			if err == nil {
				var c *net.UnixConn

				c, err = net.DialUnix("unix", nil, ua)
				if err == nil {
					// There is another running service.
					// Try to listen below and return the
					// error.
					c.Close()
				} else {
					// Remove the stale socket so the
					// listen below will succeed.
					os.Remove(socketPath)
				}
			}
		}

		oldMask := unix.Umask(0077)
		lis, err = net.Listen("unix", socketPath)
		unix.Umask(oldMask)
	} else {
		lis, err = net.Listen("tcp", ts.address)
	}

	if err != nil {
		return err
	}
	defer lis.Close()

	// Start local gRPC service on listener
	if config.Sensor.UseTLS {
		glog.V(1).Infoln("Starting telemetry server with TLS credentials")

		var certificate tls.Certificate
		certificate, err = tls.LoadX509KeyPair(config.Sensor.TLSServerCertPath, config.Sensor.TLSServerKeyPath)
		if err != nil {
			return fmt.Errorf("could not load server key pair: %s", err)
		}

		var ca []byte
		certPool := x509.NewCertPool()
		ca, err = ioutil.ReadFile(config.Sensor.TLSCACertPath)
		if err != nil {
			return fmt.Errorf("could not read ca certificate: %s", err)
		}

		if ok := certPool.AppendCertsFromPEM(ca); !ok {
			return errors.New("failed to append certs")
		}

		creds := credentials.NewTLS(&tls.Config{
			ClientAuth:   tls.RequireAndVerifyClientCert,
			Certificates: []tls.Certificate{certificate},
			ClientCAs:    certPool,
		})
		ts.server = grpc.NewServer(grpc.Creds(creds))
	} else {
		glog.V(1).Infoln("Starting telemetry server")
		ts.server = grpc.NewServer()
	}

	t := &telemetryServiceServer{
		sensor:  ts.sensor,
		service: ts,
	}
	telemetryAPI.RegisterTelemetryServiceServer(ts.server, t)

	if ts.options.start != nil {
		ts.options.start()
	}

	return ts.server.Serve(lis)
}

// Stop will stop a running TelemetryService.
func (ts *TelemetryService) Stop() {
	ts.server.Stop()
	if ts.options.stop != nil {
		ts.options.stop()
	}
}

type telemetryServiceServer struct {
	sensor  *Sensor
	service *TelemetryService
}

func (t *telemetryServiceServer) getEventsError(err error) error {
	if t.service.options.getEventsResponse != nil {
		t.service.options.getEventsResponse(nil, err)
	}
	return err
}

func (t *telemetryServiceServer) GetEvents(
	req *telemetryAPI.GetEventsRequest,
	stream telemetryAPI.TelemetryService_GetEventsServer,
) error {
	if t.service.options.getEventsRequest != nil {
		t.service.options.getEventsRequest(req)
	}

	sub := req.Subscription
	glog.V(1).Infof("GetEvents(%+v)", sub)

	if sub.EventFilter == nil {
		glog.V(1).Infof("Invalid subscription: %+v", sub)
		return t.getEventsError(errors.New("Invalid subscription (no EventFilter)"))
	}

	// Validate sub.Modifier
	var (
		err              error
		maxEvents        int64
		throttleDuration time.Duration
	)
	if sub.Modifier != nil {
		if sub.Modifier.Limit != nil {
			maxEvents = sub.Modifier.Limit.Limit
			if maxEvents < 1 {
				err = fmt.Errorf("LimitModifier is invalid (%d)",
					maxEvents)
				return t.getEventsError(err)
			}
		}
		if sub.Modifier.Throttle != nil {
			if sub.Modifier.Throttle.Interval <= 0 {
				err = fmt.Errorf("ThrottleModifier interval is invalid (%d)",
					sub.Modifier.Throttle.Interval)
				return t.getEventsError(err)
			}
			throttleDuration =
				time.Duration(sub.Modifier.Throttle.Interval)
			switch sub.Modifier.Throttle.IntervalType {
			case telemetryAPI.ThrottleModifier_MILLISECOND:
				throttleDuration *= time.Millisecond
			case telemetryAPI.ThrottleModifier_SECOND:
				throttleDuration *= time.Second
			case telemetryAPI.ThrottleModifier_MINUTE:
				throttleDuration *= time.Minute
			case telemetryAPI.ThrottleModifier_HOUR:
				throttleDuration *= time.Hour
			default:
				err = fmt.Errorf("ThrottleModifier interval type is invalid (%d)",
					sub.Modifier.Throttle.IntervalType)
				return t.getEventsError(err)
			}
		}
	}

	subscr := t.sensor.NewSubscription()
	subscr.ProcessTelemetryServiceSubscription(sub)
	if len(subscr.eventSinks) == 0 && len(subscr.status) == 0 {
		glog.V(1).Infof("Invalid subscription: %+v", sub)
		return t.getEventsError(errors.New("Invalid subscription (empty EventFilter)"))
	}

	events := make(chan TelemetryEvent, config.Sensor.ChannelBufferLength)
	f := func(e TelemetryEvent) {
		// Send the event to the data channel, but drop the event if
		// the channel is full. Do not block the sensor from delivering
		// telemetry to other subscribers.
		select {
		case events <- e:
		default:
		}
	}

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	statuses, runErr := subscr.Run(ctx, f)
	if runErr == nil || len(statuses) > 0 {
		r := &telemetryAPI.GetEventsResponse{}
		if len(statuses) == 0 {
			r.Statuses = []*status.Status{
				&status.Status{Code: int32(code.Code_OK)},
			}
		} else {
			r.Statuses = subscr.TranslateSubscriptionStatuses(statuses)
		}
		if err = stream.Send(r); err != nil {
			return t.getEventsError(err)
		}
		if t.service.options.getEventsResponse != nil {
			t.service.options.getEventsResponse(r, nil)
		}
	}
	if runErr != nil {
		glog.Errorf("Failed to get events for subscription %+v: %v",
			sub, runErr)
		return runErr
	}

	var nEvents int64
	nextEventTime := time.Now()
	for {
		select {
		case <-ctx.Done():
			glog.V(1).Infof("Client disconnected, closing stream")
			return ctx.Err()
		case e := <-events:
			r := &telemetryAPI.GetEventsResponse{}
			if e != nil {
				if throttleDuration != 0 {
					now := time.Now()
					if now.Before(nextEventTime) {
						break
					}
					nextEventTime = now
					nextEventTime.Add(throttleDuration)
				}
				r.Events = []*telemetryAPI.ReceivedTelemetryEvent{
					&telemetryAPI.ReceivedTelemetryEvent{
						Event: subscr.TranslateTelemetryEvent(e),
					},
				}
			}
			r.Statuses = subscr.TranslateSubscriptionStatuses(subscr.GetStatuses())
			if err = stream.Send(r); err != nil {
				return err
			}
			if maxEvents > 0 {
				nEvents++
				if nEvents == maxEvents {
					return fmt.Errorf("Event limit reached (%d)",
						maxEvents)
				}
			}
		}
	}
}

// TranslateSubscriptionStatuses translates status information from a
// subscription for delivery to a telemetry client.
func (s *Subscription) TranslateSubscriptionStatuses(
	statuses []string,
) []*status.Status {
	if len(statuses) == 0 {
		return nil
	}
	result := make([]*status.Status, len(statuses))
	for i, s := range statuses {
		result[i] = &status.Status{
			Code:    int32(code.Code_UNKNOWN),
			Message: s,
		}
	}
	return result
}

// ProcessTelemetryServiceSubscription processes a Subscription message from
// the telemetry service API.
func (s *Subscription) ProcessTelemetryServiceSubscription(sub *telemetryAPI.Subscription) {
	if sub.ContainerFilter != nil {
		cf := NewContainerFilter()
		for _, id := range sub.ContainerFilter.Ids {
			cf.AddContainerID(id)
		}
		for _, name := range sub.ContainerFilter.Names {
			cf.AddContainerName(name)
		}
		for _, id := range sub.ContainerFilter.ImageIds {
			cf.AddImageID(id)
		}
		for _, name := range sub.ContainerFilter.ImageNames {
			cf.AddImageName(name)
		}
		if cf.Len() > 0 {
			s.SetContainerFilter(cf)
		}
	}

	s.registerChargenEvents(sub.EventFilter.ChargenEvents)
	s.registerContainerEvents(sub.EventFilter.ContainerEvents)
	s.registerFileEvents(sub.EventFilter.FileEvents)
	s.registerKernelFunctionCallEvents(sub.EventFilter.KernelEvents)
	s.registerNetworkEvents(sub.EventFilter.NetworkEvents)
	s.registerPerformanceEvents(sub.EventFilter.PerformanceEvents)
	s.registerProcessEvents(sub.EventFilter.ProcessEvents)
	s.registerSyscallEvents(sub.EventFilter.SyscallEvents)
	s.registerTickerEvents(sub.EventFilter.TickerEvents)
	s.registerUserFunctionCallEvents(sub.EventFilter.UserEvents)
}

func (s *Subscription) registerChargenEvents(events []*telemetryAPI.ChargenEventFilter) {
	for _, e := range events {
		s.RegisterChargenEventFilter(e.Length, nil)
	}
}

func (s *Subscription) registerContainerEvents(events []*telemetryAPI.ContainerEventFilter) {
	type registerFunc func(*expression.Expression)

	var (
		filters       [6]*telemetryAPI.Expression
		subscriptions [6]registerFunc
		views         [6]bool
		wildcards     [6]bool
	)

	for _, e := range events {
		t := e.GetType()
		if t < 1 || t > 5 {
			s.logStatus(
				fmt.Sprintf("ContainerEventType %d is invalid", t))
			continue
		}

		if subscriptions[t] == nil {
			switch t {
			case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_CREATED:
				subscriptions[t] = s.RegisterContainerCreatedEventFilter
			case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_RUNNING:
				subscriptions[t] = s.RegisterContainerRunningEventFilter
			case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_EXITED:
				subscriptions[t] = s.RegisterContainerExitedEventFilter
			case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_DESTROYED:
				subscriptions[t] = s.RegisterContainerDestroyedEventFilter
			case telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_UPDATED:
				subscriptions[t] = s.RegisterContainerUpdatedEventFilter
			}
		}
		if e.View == telemetryAPI.ContainerEventView_FULL {
			views[t] = true
		}
		if e.FilterExpression == nil {
			wildcards[t] = true
			filters[t] = nil
		} else if !wildcards[t] {
			filters[t] = expression.LogicalOr(
				e.FilterExpression,
				filters[t])
		}
	}

	// FIXME do something with views!

	for i, f := range subscriptions {
		if f == nil {
			continue
		}
		if wildcards[i] {
			f(nil)
		} else if expr, err := expression.ConvertExpression(filters[i], ContainerEventTypes); err == nil {
			f(expr)
		} else {
			s.logStatus(
				fmt.Sprintf("Invalid container filter expression: %v", err))
		}
	}
}

func rewriteFileEventFilter(fef *telemetryAPI.FileEventFilter) {
	if fef.Filename != nil {
		newExpr := expression.Equal(
			expression.Identifier("filename"),
			expression.Value(fef.Filename.Value))
		fef.FilterExpression = expression.LogicalAnd(
			fef.FilterExpression, newExpr)
		fef.Filename = nil
		fef.FilenamePattern = nil
	} else if fef.FilenamePattern != nil {
		newExpr := expression.Like(
			expression.Identifier("filename"),
			expression.Value(fef.FilenamePattern.Value))
		fef.FilterExpression = expression.LogicalAnd(
			fef.FilterExpression, newExpr)
		fef.FilenamePattern = nil
	}

	if fef.OpenFlagsMask != nil {
		newExpr := expression.NotEqual(
			expression.BitwiseAnd(
				expression.Identifier("flags"),
				expression.Value(fef.OpenFlagsMask.Value)),
			expression.Value(int32(0)))
		fef.FilterExpression = expression.LogicalAnd(
			fef.FilterExpression, newExpr)
		fef.OpenFlagsMask = nil
	}

	if fef.CreateModeMask != nil {
		newExpr := expression.NotEqual(
			expression.BitwiseAnd(
				expression.Identifier("mode"),
				expression.Value(fef.CreateModeMask.Value)),
			expression.Value(int32(0)))
		fef.FilterExpression = expression.LogicalAnd(
			fef.FilterExpression, newExpr)
		fef.CreateModeMask = nil
	}
}

func (s *Subscription) registerFileEvents(events []*telemetryAPI.FileEventFilter) {
	type registerFunc func(*expression.Expression)

	var (
		filters       [10]*telemetryAPI.Expression
		subscriptions [10]registerFunc
		types         [10]expression.FieldTypeMap
		wildcards     [10]bool
	)

	for _, e := range events {
		// Translate deprecated fields into an expression
		rewriteFileEventFilter(e)

		t := e.GetType()
		if t < 1 || t > telemetryAPI.FileEventType(len(subscriptions)-1) {
			s.logStatus(
				fmt.Sprintf("FileEventType %d is invalid", t))
			continue
		}

		if subscriptions[t] == nil {
			switch t {
			case telemetryAPI.FileEventType_FILE_EVENT_TYPE_CREATE:
				subscriptions[t] = s.RegisterFileCreateEventFilter
				types[t] = FileCreateEventTypes
			case telemetryAPI.FileEventType_FILE_EVENT_TYPE_DELETE:
				subscriptions[t] = s.RegisterFileDeleteEventFilter
				types[t] = FileDeleteEventTypes
			case telemetryAPI.FileEventType_FILE_EVENT_TYPE_LINK:
				subscriptions[t] = s.RegisterFileLinkEventFilter
				types[t] = FileLinkEventTypes
			case telemetryAPI.FileEventType_FILE_EVENT_TYPE_MODIFY:
				subscriptions[t] = s.RegisterFileModifyEventFilter
				types[t] = FileModifyEventTypes
			case telemetryAPI.FileEventType_FILE_EVENT_TYPE_OPEN:
				subscriptions[t] = s.RegisterFileOpenEventFilter
				types[t] = FileOpenEventTypes
			case telemetryAPI.FileEventType_FILE_EVENT_TYPE_RENAME:
				subscriptions[t] = s.RegisterFileRenameEventFilter
				types[t] = FileRenameEventTypes
			case telemetryAPI.FileEventType_FILE_EVENT_TYPE_OPEN_FOR_MODIFY:
				subscriptions[t] = s.RegisterFileOpenForModifyEventFilter
				types[t] = FileOpenForModifyEventTypes
			case telemetryAPI.FileEventType_FILE_EVENT_TYPE_CLOSE_FOR_MODIFY:
				subscriptions[t] = s.RegisterFileCloseForModifyEventFilter
				types[t] = FileCloseForModifyEventTypes
			case telemetryAPI.FileEventType_FILE_EVENT_TYPE_ATTRIBUTE_CHANGE:
				subscriptions[t] = s.RegisterFileAttributeChangeEventFilter
				types[t] = FileAttributeChangeEventTypes
			}
		}
		if e.FilterExpression == nil {
			wildcards[t] = true
			filters[t] = nil
		} else if !wildcards[t] {
			filters[t] = expression.LogicalOr(
				e.FilterExpression,
				filters[t])
		}
	}

	for i, f := range subscriptions {
		if f == nil {
			continue
		}
		if wildcards[i] {
			f(nil)
		} else if expr, err := expression.ConvertExpression(filters[i], types[i]); err == nil {
			f(expr)
		} else {
			s.logStatus(
				fmt.Sprintf("Invalid file filter expression: %v", err))
		}
	}
}

func (s *Subscription) registerKernelFunctionCallEvents(events []*telemetryAPI.KernelFunctionCallFilter) {
	for _, e := range events {
		var onReturn bool
		switch e.Type {
		case telemetryAPI.KernelFunctionCallEventType_KERNEL_FUNCTION_CALL_EVENT_TYPE_ENTER:
			onReturn = false
		case telemetryAPI.KernelFunctionCallEventType_KERNEL_FUNCTION_CALL_EVENT_TYPE_EXIT:
			onReturn = true
		default:
			s.logStatus(
				fmt.Sprintf("KernelFunctionCallEventType %d is invalid", e.Type))
			continue
		}

		var filterExpression *expression.Expression
		if expr := e.GetFilterExpression(); expr != nil {
			var err error
			// Types cannot be bound here, because they're dynamic
			// This is a special case for now and will be done later.
			filterExpression, err = expression.ConvertExpression(expr, nil)
			if err != nil {
				s.logStatus(
					fmt.Sprintf("Invalid filter expression for kernel function call filter: %v", err))
				continue
			}
		}

		s.RegisterKernelFunctionCallEventFilter(e.Symbol, onReturn,
			e.Arguments, filterExpression)
	}
}

type networkFilterItem struct {
	filter   *telemetryAPI.Expression
	wildcard bool
}

func (nfi *networkFilterItem) add(nef *telemetryAPI.NetworkEventFilter) {
	if nef.FilterExpression == nil {
		nfi.wildcard = true
		nfi.filter = nil
	} else if nfi.wildcard == false {
		nfi.filter = expression.LogicalOr(nef.FilterExpression,
			nfi.filter)
	}
}

type networkFilterItemRegisterFunc func(*expression.Expression)

func (nfi *networkFilterItem) register(
	s *Subscription,
	f networkFilterItemRegisterFunc,
	t expression.FieldTypeMap,
) {
	if nfi != nil {
		if nfi.wildcard {
			f(nil)
		} else if nfi.filter != nil {
			if expr, err := expression.ConvertExpression(nfi.filter, t); err != nil {
				s.logStatus(
					fmt.Sprintf("Invalid filter expression for network accept attempt filter: %v", err))
			} else {
				f(expr)
			}
		}
	}
}

type networkFilterSet struct {
	acceptAttemptFilters   networkFilterItem
	acceptResultFilters    networkFilterItem
	bindAttemptFilters     networkFilterItem
	bindResultFilters      networkFilterItem
	connectAttemptFilters  networkFilterItem
	connectResultFilters   networkFilterItem
	listenAttemptFilters   networkFilterItem
	listenResultFilters    networkFilterItem
	recvfromAttemptFilters networkFilterItem
	recvfromResultFilters  networkFilterItem
	sendtoAttemptFilters   networkFilterItem
	sendtoResultFilters    networkFilterItem
}

func (nfs *networkFilterSet) add(
	subscr *Subscription,
	nef *telemetryAPI.NetworkEventFilter,
) {
	switch nef.Type {
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_ACCEPT_ATTEMPT:
		nfs.acceptAttemptFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_ACCEPT_RESULT:
		nfs.acceptResultFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_BIND_ATTEMPT:
		nfs.bindAttemptFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_BIND_RESULT:
		nfs.bindResultFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_CONNECT_ATTEMPT:
		nfs.connectAttemptFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_CONNECT_RESULT:
		nfs.connectResultFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_LISTEN_ATTEMPT:
		nfs.listenAttemptFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_LISTEN_RESULT:
		nfs.listenResultFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_RECVFROM_ATTEMPT:
		nfs.recvfromAttemptFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_RECVFROM_RESULT:
		nfs.recvfromResultFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_SENDTO_ATTEMPT:
		nfs.sendtoAttemptFilters.add(nef)
	case telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_SENDTO_RESULT:
		nfs.sendtoResultFilters.add(nef)
	default:
		subscr.logStatus(
			fmt.Sprintf("Invalid NetworkEventType %d", nef.Type))
	}
}

func (s *Subscription) registerNetworkEvents(events []*telemetryAPI.NetworkEventFilter) {
	nfs := networkFilterSet{}
	for _, nef := range events {
		nfs.add(s, nef)
	}

	nfs.acceptAttemptFilters.register(s, s.RegisterNetworkAcceptAttemptEventFilter, NetworkAttemptEventTypes)
	nfs.acceptResultFilters.register(s, s.RegisterNetworkAcceptResultEventFilter, NetworkResultEventTypes)
	nfs.bindAttemptFilters.register(s, s.RegisterNetworkBindAttemptEventFilter, NetworkAttemptWithAddressEventTypes)
	nfs.bindResultFilters.register(s, s.RegisterNetworkBindResultEventFilter, NetworkResultEventTypes)
	nfs.connectAttemptFilters.register(s, s.RegisterNetworkConnectAttemptEventFilter, NetworkAttemptWithAddressEventTypes)
	nfs.connectResultFilters.register(s, s.RegisterNetworkConnectResultEventFilter, NetworkResultEventTypes)
	nfs.listenAttemptFilters.register(s, s.RegisterNetworkListenAttemptEventFilter, NetworkListenAttemptEventTypes)
	nfs.listenResultFilters.register(s, s.RegisterNetworkListenResultEventFilter, NetworkResultEventTypes)
	nfs.recvfromAttemptFilters.register(s, s.RegisterNetworkRecvfromAttemptEventFilter, NetworkAttemptEventTypes)
	nfs.recvfromResultFilters.register(s, s.RegisterNetworkRecvfromResultEventFilter, NetworkResultEventTypes)
	nfs.sendtoAttemptFilters.register(s, s.RegisterNetworkSendtoAttemptEventFilter, NetworkAttemptWithAddressEventTypes)
	nfs.sendtoResultFilters.register(s, s.RegisterNetworkSendtoResultEventFilter, NetworkResultEventTypes)
}

func (s *Subscription) registerPerformanceEvents(events []*telemetryAPI.PerformanceEventFilter) {
	for _, e := range events {
		attr := perf.EventAttr{
			SampleType: perf.PERF_SAMPLE_CPU | perf.PERF_SAMPLE_RAW,
		}
		switch e.SampleRateType {
		case telemetryAPI.SampleRateType_SAMPLE_RATE_TYPE_PERIOD:
			if rate, ok := e.SampleRate.(*telemetryAPI.PerformanceEventFilter_Period); !ok {
				s.logStatus(
					fmt.Sprintf("Period not properly specified for periodic sample rate"))
				continue
			} else {
				attr.SamplePeriod = rate.Period
				attr.Freq = false
			}
		case telemetryAPI.SampleRateType_SAMPLE_RATE_TYPE_FREQUENCY:
			if rate, ok := e.SampleRate.(*telemetryAPI.PerformanceEventFilter_Frequency); !ok {
				s.logStatus(
					fmt.Sprintf("Frequency not properly specified for frequency sample rate"))
				continue
			} else {
				attr.SampleFreq = rate.Frequency
				attr.Freq = true
			}
		default:
			s.logStatus(
				fmt.Sprintf("SampleRateType %d is invalid", e.SampleRateType))
			continue
		}

		counters := make([]perf.CounterEventGroupMember, 0, len(e.Events))
		for _, ev := range e.Events {
			m := perf.CounterEventGroupMember{
				Config: ev.Config,
			}
			switch ev.Type {
			case telemetryAPI.PerformanceEventType_PERFORMANCE_EVENT_TYPE_HARDWARE:
				m.EventType = perf.EventTypeHardware
			case telemetryAPI.PerformanceEventType_PERFORMANCE_EVENT_TYPE_HARDWARE_CACHE:
				m.EventType = perf.EventTypeHardwareCache
			case telemetryAPI.PerformanceEventType_PERFORMANCE_EVENT_TYPE_SOFTWARE:
				m.EventType = perf.EventTypeSoftware
			default:
				s.logStatus(
					fmt.Sprintf("PerformanceEventType %d is invalid", ev.Type))
				continue
			}
			counters = append(counters, m)
		}

		s.RegisterPerformanceEventFilter(attr, counters)
	}
}

func rewriteProcessEventFilter(pef *telemetryAPI.ProcessEventFilter) {
	switch pef.Type {
	case telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXEC:
		if pef.ExecFilename != nil {
			newExpr := expression.Equal(
				expression.Identifier("filename"),
				expression.Value(pef.ExecFilename.Value))
			pef.FilterExpression = expression.LogicalAnd(
				pef.FilterExpression, newExpr)
			pef.ExecFilename = nil
			pef.ExecFilenamePattern = nil
		} else if pef.ExecFilenamePattern != nil {
			newExpr := expression.Like(
				expression.Identifier("filename"),
				expression.Value(pef.ExecFilenamePattern.Value))
			pef.FilterExpression = expression.LogicalAnd(
				pef.FilterExpression, newExpr)
			pef.ExecFilenamePattern = nil
		}
	case telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXIT:
		if pef.ExitCode != nil {
			newExpr := expression.Equal(
				expression.Identifier("code"),
				expression.Value(pef.ExitCode.Value))
			pef.FilterExpression = expression.LogicalAnd(
				pef.FilterExpression, newExpr)
			pef.ExitCode = nil
		}
	}
}

func (s *Subscription) registerProcessEvents(events []*telemetryAPI.ProcessEventFilter) {
	type registerFunc func(*expression.Expression)

	var (
		filters       [5]*telemetryAPI.Expression
		subscriptions [5]registerFunc
		types         [5]expression.FieldTypeMap
		wildcards     [5]bool
	)

	for _, e := range events {
		// Translate deprecated fields into an expression
		rewriteProcessEventFilter(e)

		t := e.GetType()
		if t < 1 || t > telemetryAPI.ProcessEventType(len(subscriptions)-1) {
			s.logStatus(
				fmt.Sprintf("ProcessEventType %d is invalid", t))
			continue
		}

		if subscriptions[t] == nil {
			switch t {
			case telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXEC:
				subscriptions[t] = s.RegisterProcessExecEventFilter
				types[t] = ProcessExecEventTypes
			case telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_FORK:
				subscriptions[t] = s.RegisterProcessForkEventFilter
				types[t] = ProcessForkEventTypes
			case telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXIT:
				subscriptions[t] = s.RegisterProcessExitEventFilter
				types[t] = ProcessExitEventTypes
			case telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_UPDATE:
				subscriptions[t] = s.RegisterProcessUpdateEventFilter
				types[t] = ProcessUpdateEventTypes
			}
		}
		if e.FilterExpression == nil {
			wildcards[t] = true
			filters[t] = nil
		} else if !wildcards[t] {
			filters[t] = expression.LogicalOr(
				e.FilterExpression,
				filters[t])
		}
	}

	for i, f := range subscriptions {
		if f == nil {
			continue
		}
		if wildcards[i] {
			f(nil)
		} else if expr, err := expression.ConvertExpression(filters[i], types[i]); err == nil {
			f(expr)
		} else {
			s.logStatus(
				fmt.Sprintf("Invalid process filter expression: %v", err))
		}
	}
}

func rewriteSyscallEventFilter(sef *telemetryAPI.SyscallEventFilter) {
	if sef.Id != nil {
		newExpr := expression.Equal(
			expression.Identifier("id"),
			expression.Value(sef.Id.Value))
		sef.FilterExpression = expression.LogicalAnd(
			sef.FilterExpression, newExpr)
		sef.Id = nil
	}

	if sef.Type == telemetryAPI.SyscallEventType_SYSCALL_EVENT_TYPE_ENTER {
		if sef.Arg0 != nil {
			newExpr := expression.Equal(
				expression.Identifier("arg0"),
				expression.Value(sef.Arg0.Value))
			sef.FilterExpression = expression.LogicalAnd(
				sef.FilterExpression, newExpr)
			sef.Arg0 = nil
		}

		if sef.Arg1 != nil {
			newExpr := expression.Equal(
				expression.Identifier("arg1"),
				expression.Value(sef.Arg1.Value))
			sef.FilterExpression = expression.LogicalAnd(
				sef.FilterExpression, newExpr)
			sef.Arg1 = nil
		}

		if sef.Arg2 != nil {
			newExpr := expression.Equal(
				expression.Identifier("arg2"),
				expression.Value(sef.Arg2.Value))
			sef.FilterExpression = expression.LogicalAnd(
				sef.FilterExpression, newExpr)
			sef.Arg2 = nil
		}

		if sef.Arg3 != nil {
			newExpr := expression.Equal(
				expression.Identifier("arg3"),
				expression.Value(sef.Arg3.Value))
			sef.FilterExpression = expression.LogicalAnd(
				sef.FilterExpression, newExpr)
			sef.Arg3 = nil
		}

		if sef.Arg4 != nil {
			newExpr := expression.Equal(
				expression.Identifier("arg4"),
				expression.Value(sef.Arg4.Value))
			sef.FilterExpression = expression.LogicalAnd(
				sef.FilterExpression, newExpr)
			sef.Arg4 = nil
		}

		if sef.Arg5 != nil {
			newExpr := expression.Equal(
				expression.Identifier("arg5"),
				expression.Value(sef.Arg5.Value))
			sef.FilterExpression = expression.LogicalAnd(
				sef.FilterExpression, newExpr)
			sef.Arg5 = nil
		}
	} else if sef.Type == telemetryAPI.SyscallEventType_SYSCALL_EVENT_TYPE_EXIT {
		if sef.Ret != nil {
			newExpr := expression.Equal(
				expression.Identifier("ret"),
				expression.Value(sef.Ret.Value))
			sef.FilterExpression = expression.LogicalAnd(
				sef.FilterExpression, newExpr)
			sef.Ret = nil
		}
	}
}

func containsIDFilter(expr *telemetryAPI.Expression) bool {
	if expr != nil {
		switch expr.GetType() {
		case telemetryAPI.Expression_LOGICAL_AND:
			operands := expr.GetBinaryOp()
			return containsIDFilter(operands.Lhs) ||
				containsIDFilter(operands.Rhs)
		case telemetryAPI.Expression_LOGICAL_OR:
			operands := expr.GetBinaryOp()
			return containsIDFilter(operands.Lhs) &&
				containsIDFilter(operands.Rhs)
		case telemetryAPI.Expression_EQ:
			operands := expr.GetBinaryOp()
			if operands.Lhs.GetType() != telemetryAPI.Expression_IDENTIFIER {
				return false
			}
			if operands.Lhs.GetIdentifier() != "id" {
				return false
			}
			return true
		}
	}
	return false
}

func (s *Subscription) registerSyscallEvents(events []*telemetryAPI.SyscallEventFilter) {
	var enterFilter, exitFilter *telemetryAPI.Expression

	for _, e := range events {
		// Translate deprecated fields into an expression
		rewriteSyscallEventFilter(e)

		if !containsIDFilter(e.FilterExpression) {
			// No wildcard filters for now
			s.logStatus(
				"Wildcard syscall filter ignored")
			continue
		}

		switch t := e.GetType(); t {
		case telemetryAPI.SyscallEventType_SYSCALL_EVENT_TYPE_ENTER:
			enterFilter = expression.LogicalOr(enterFilter,
				e.FilterExpression)
		case telemetryAPI.SyscallEventType_SYSCALL_EVENT_TYPE_EXIT:
			exitFilter = expression.LogicalOr(exitFilter,
				e.FilterExpression)
		default:
			s.logStatus(
				fmt.Sprintf("SyscallEventType %d is invalid", t))
			continue
		}
	}

	if enterFilter != nil {
		if expr, err := expression.ConvertExpression(enterFilter, SyscallEnterEventTypes); err == nil {
			s.RegisterSyscallEnterEventFilter(expr)
		} else {
			s.logStatus(
				fmt.Sprintf("Invalid filter expression for syscall enter filter: %v", err))
		}
	}
	if exitFilter != nil {
		if expr, err := expression.ConvertExpression(exitFilter, SyscallExitEventTypes); err == nil {
			s.RegisterSyscallExitEventFilter(expr)
		} else {
			s.logStatus(
				fmt.Sprintf("Invalid filter expression for syscall exit filter: %v", err))
		}
	}
}

func (s *Subscription) registerTickerEvents(events []*telemetryAPI.TickerEventFilter) {
	for _, e := range events {
		s.RegisterTickerEventFilter(e.Interval, nil)
	}
}

func (s *Subscription) registerUserFunctionCallEvents(events []*telemetryAPI.UserFunctionCallFilter) {
	for _, e := range events {
		var onReturn bool
		switch e.Type {
		case telemetryAPI.UserFunctionCallEventType_USER_FUNCTION_CALL_EVENT_TYPE_ENTER:
			onReturn = false
		case telemetryAPI.UserFunctionCallEventType_USER_FUNCTION_CALL_EVENT_TYPE_EXIT:
			onReturn = true
		default:
			s.logStatus(
				fmt.Sprintf("UserFunctionCallEventType %d is invalid", e.Type))
			continue
		}

		var filterExpression *expression.Expression
		if expr := e.GetFilterExpression(); expr != nil {
			var err error
			// Types cannot be bound here, because they're dynamic
			// This is a special case for now and will be done later.
			filterExpression, err = expression.ConvertExpression(expr, nil)
			if err != nil {
				s.logStatus(
					fmt.Sprintf("Invalid filter expression for user function call filter: %v", err))
				continue
			}
		}

		s.RegisterUserFunctionCallEventFilter(e.Executable, e.Symbol, onReturn,
			e.Arguments, filterExpression)
	}
}

// NewTelemetryEvent creates a filled TelemetryEvent from a TelemetryEventData
func NewTelemetryEvent(e TelemetryEventData) *telemetryAPI.TelemetryEvent {
	event := &telemetryAPI.TelemetryEvent{
		Id:                   e.EventID,
		ProcessId:            e.ProcessID,
		ProcessPid:           int32(e.PID),
		ContainerId:          e.Container.ID,
		SensorId:             e.SensorID,
		SensorSequenceNumber: e.SequenceNumber,
		SensorMonotimeNanos:  e.MonotimeNanos,
		ContainerName:        e.Container.Name,
		ImageId:              e.Container.ImageID,
		ImageName:            e.Container.ImageName,
		Cpu:                  int32(e.CPU),
		ProcessTgid:          int32(e.TGID),
	}

	if e.HasCredentials {
		event.Credentials = &telemetryAPI.Credentials{
			Uid:   e.Credentials.UID,
			Gid:   e.Credentials.GID,
			Euid:  e.Credentials.EUID,
			Egid:  e.Credentials.EGID,
			Suid:  e.Credentials.SUID,
			Sgid:  e.Credentials.SGID,
			Fsuid: e.Credentials.FSUID,
			Fsgid: e.Credentials.FSGID,
		}
	}

	return event
}

func translateNetworkAddress(addr NetworkAddressTelemetryEventData) *telemetryAPI.NetworkAddress {
	switch addr.Family {
	case unix.AF_LOCAL:
		return &telemetryAPI.NetworkAddress{
			Family: telemetryAPI.NetworkAddressFamily_NETWORK_ADDRESS_FAMILY_LOCAL,
			Address: &telemetryAPI.NetworkAddress_LocalAddress{
				LocalAddress: addr.UnixPath,
			},
		}
	case unix.AF_INET:
		return &telemetryAPI.NetworkAddress{
			Family: telemetryAPI.NetworkAddressFamily_NETWORK_ADDRESS_FAMILY_INET,
			Address: &telemetryAPI.NetworkAddress_Ipv4Address{
				Ipv4Address: &telemetryAPI.IPv4AddressAndPort{
					Address: &telemetryAPI.IPv4Address{
						Address: addr.IPv4Address,
					},
					Port: uint32(addr.IPv4Port),
				},
			},
		}
	case unix.AF_INET6:
		return &telemetryAPI.NetworkAddress{
			Family: telemetryAPI.NetworkAddressFamily_NETWORK_ADDRESS_FAMILY_INET6,
			Address: &telemetryAPI.NetworkAddress_Ipv6Address{
				Ipv6Address: &telemetryAPI.IPv6AddressAndPort{
					Address: &telemetryAPI.IPv6Address{
						High: addr.IPv6AddressHigh,
						Low:  addr.IPv6AddressLow,
					},
					Port: uint32(addr.IPv6Port),
				},
			},
		}
	}
	return nil
}

// TranslateTelemetryEvent translates a sensor telemetry event into a telemetry
// service TelemetryEvent.
func (s *Subscription) TranslateTelemetryEvent(ev TelemetryEvent) *telemetryAPI.TelemetryEvent {
	eventData := ev.CommonTelemetryEventData()
	if len(eventData.Container.ID) > 0 && len(eventData.Container.Name) == 0 {
		// We have a container ID without a name. Let's see if
		// we can refresh that.
		ci := s.sensor.ContainerCache.LookupContainer(
			eventData.Container.ID, false)
		if ci != nil {
			eventData.Container = *ci
		}
	}
	event := NewTelemetryEvent(eventData)

	switch e := ev.(type) {
	case ChargenTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Chargen{
			Chargen: &telemetryAPI.ChargenEvent{
				Index:      e.Index,
				Characters: e.Characters,
			},
		}

	case ContainerCreatedTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Container{
			Container: &telemetryAPI.ContainerEvent{
				Type:             telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_CREATED,
				Name:             e.Container.Name,
				ImageId:          e.Container.ImageID,
				ImageName:        e.Container.ImageName,
				HostPid:          int32(e.Container.Pid),
				DockerConfigJson: e.Container.JSONConfig,
				OciConfigJson:    e.Container.OCIConfig,
			},
		}

	case ContainerDestroyedTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Container{
			Container: &telemetryAPI.ContainerEvent{
				Type:             telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_DESTROYED,
				Name:             e.Container.Name,
				ImageId:          e.Container.ImageID,
				ImageName:        e.Container.ImageName,
				HostPid:          int32(e.Container.Pid),
				DockerConfigJson: e.Container.JSONConfig,
				OciConfigJson:    e.Container.OCIConfig,
			},
		}
	case ContainerExitedTelemetryEvent:
		var exitSignal, exitStatus uint32
		ws := unix.WaitStatus(e.Container.ExitCode)
		if ws.Exited() {
			exitStatus = uint32(ws.ExitStatus())
		}
		if ws.Signaled() {
			exitSignal = uint32(ws.Signal())
		}
		event.Event = &telemetryAPI.TelemetryEvent_Container{
			Container: &telemetryAPI.ContainerEvent{
				Type:             telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_EXITED,
				Name:             e.Container.Name,
				ImageId:          e.Container.ImageID,
				ImageName:        e.Container.ImageName,
				HostPid:          int32(e.Container.Pid),
				ExitCode:         int32(e.Container.ExitCode),
				ExitStatus:       exitStatus,
				ExitSignal:       exitSignal,
				ExitCoreDumped:   ws.CoreDump(),
				DockerConfigJson: e.Container.JSONConfig,
				OciConfigJson:    e.Container.OCIConfig,
			},
		}
	case ContainerRunningTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Container{
			Container: &telemetryAPI.ContainerEvent{
				Type:             telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_RUNNING,
				Name:             e.Container.Name,
				ImageId:          e.Container.ImageID,
				ImageName:        e.Container.ImageName,
				HostPid:          int32(e.Container.Pid),
				DockerConfigJson: e.Container.JSONConfig,
				OciConfigJson:    e.Container.OCIConfig,
			},
		}

	case ContainerUpdatedTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Container{
			Container: &telemetryAPI.ContainerEvent{
				Type:             telemetryAPI.ContainerEventType_CONTAINER_EVENT_TYPE_UPDATED,
				Name:             e.Container.Name,
				ImageId:          e.Container.ImageID,
				ImageName:        e.Container.ImageName,
				HostPid:          int32(e.Container.Pid),
				DockerConfigJson: e.Container.JSONConfig,
				OciConfigJson:    e.Container.OCIConfig,
			},
		}

	case FileCreateTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_File{
			File: &telemetryAPI.FileEvent{
				Type:     telemetryAPI.FileEventType_FILE_EVENT_TYPE_CREATE,
				Filename: e.Filename,
				OpenMode: e.Mode,
			},
		}

	case FileDeleteTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_File{
			File: &telemetryAPI.FileEvent{
				Type:     telemetryAPI.FileEventType_FILE_EVENT_TYPE_DELETE,
				Filename: e.Filename,
			},
		}

	case FileLinkTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_File{
			File: &telemetryAPI.FileEvent{
				Type:       telemetryAPI.FileEventType_FILE_EVENT_TYPE_LINK,
				SourceFile: e.SourceFile,
				TargetFile: e.TargetFile,
				Symlink:    e.Symlink,
			},
		}

	case FileModifyTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_File{
			File: &telemetryAPI.FileEvent{
				Type:     telemetryAPI.FileEventType_FILE_EVENT_TYPE_MODIFY,
				Filename: e.Filename,
			},
		}

	case FileOpenTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_File{
			File: &telemetryAPI.FileEvent{
				Type:      telemetryAPI.FileEventType_FILE_EVENT_TYPE_OPEN,
				Filename:  e.Filename,
				OpenFlags: e.Flags,
				OpenMode:  e.Mode,
			},
		}

	case FileRenameTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_File{
			File: &telemetryAPI.FileEvent{
				Type:    telemetryAPI.FileEventType_FILE_EVENT_TYPE_RENAME,
				Oldname: e.Oldname,
				Newname: e.Newname,
			},
		}

	case FileOpenForModifyTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_File{
			File: &telemetryAPI.FileEvent{
				Type:     telemetryAPI.FileEventType_FILE_EVENT_TYPE_OPEN_FOR_MODIFY,
				Filename: e.Filename,
			},
		}

	case FileCloseForModifyTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_File{
			File: &telemetryAPI.FileEvent{
				Type:     telemetryAPI.FileEventType_FILE_EVENT_TYPE_CLOSE_FOR_MODIFY,
				Filename: e.Filename,
			},
		}

	case FileAttributeChangeTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_File{
			File: &telemetryAPI.FileEvent{
				Type:     telemetryAPI.FileEventType_FILE_EVENT_TYPE_ATTRIBUTE_CHANGE,
				Filename: e.Filename,
			},
		}

	case KernelFunctionCallTelemetryEvent:
		args := make(map[string]*telemetryAPI.KernelFunctionCallEvent_FieldValue)
		for k, v := range e.Arguments {
			value := &telemetryAPI.KernelFunctionCallEvent_FieldValue{}
			switch v := v.(type) {
			case []byte:
				value.FieldType = telemetryAPI.KernelFunctionCallEvent_BYTES
				value.Value = &telemetryAPI.KernelFunctionCallEvent_FieldValue_BytesValue{BytesValue: v}
			case string:
				value.FieldType = telemetryAPI.KernelFunctionCallEvent_STRING
				value.Value = &telemetryAPI.KernelFunctionCallEvent_FieldValue_StringValue{StringValue: v}
			case int8:
				value.FieldType = telemetryAPI.KernelFunctionCallEvent_SINT8
				value.Value = &telemetryAPI.KernelFunctionCallEvent_FieldValue_SignedValue{SignedValue: int64(v)}
			case int16:
				value.FieldType = telemetryAPI.KernelFunctionCallEvent_SINT16
				value.Value = &telemetryAPI.KernelFunctionCallEvent_FieldValue_SignedValue{SignedValue: int64(v)}
			case int32:
				value.FieldType = telemetryAPI.KernelFunctionCallEvent_SINT32
				value.Value = &telemetryAPI.KernelFunctionCallEvent_FieldValue_SignedValue{SignedValue: int64(v)}
			case int64:
				value.FieldType = telemetryAPI.KernelFunctionCallEvent_SINT64
				value.Value = &telemetryAPI.KernelFunctionCallEvent_FieldValue_SignedValue{SignedValue: v}
			case uint8:
				value.FieldType = telemetryAPI.KernelFunctionCallEvent_UINT8
				value.Value = &telemetryAPI.KernelFunctionCallEvent_FieldValue_UnsignedValue{UnsignedValue: uint64(v)}
			case uint16:
				value.FieldType = telemetryAPI.KernelFunctionCallEvent_UINT16
				value.Value = &telemetryAPI.KernelFunctionCallEvent_FieldValue_UnsignedValue{UnsignedValue: uint64(v)}
			case uint32:
				value.FieldType = telemetryAPI.KernelFunctionCallEvent_UINT32
				value.Value = &telemetryAPI.KernelFunctionCallEvent_FieldValue_UnsignedValue{UnsignedValue: uint64(v)}
			case uint64:
				value.FieldType = telemetryAPI.KernelFunctionCallEvent_UINT64
				value.Value = &telemetryAPI.KernelFunctionCallEvent_FieldValue_UnsignedValue{UnsignedValue: v}
			}
			args[k] = value
		}
		event.Event = &telemetryAPI.TelemetryEvent_KernelCall{
			KernelCall: &telemetryAPI.KernelFunctionCallEvent{
				Arguments: args,
			},
		}

	case NetworkAcceptAttemptTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:   telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_ACCEPT_ATTEMPT,
				Sockfd: e.FD,
			},
		}

	case NetworkAcceptResultTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:   telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_ACCEPT_RESULT,
				Result: e.Return,
			},
		}

	case NetworkBindAttemptTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:    telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_BIND_ATTEMPT,
				Sockfd:  e.FD,
				Address: translateNetworkAddress(e.NetworkAddressTelemetryEventData),
			},
		}

	case NetworkBindResultTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:   telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_BIND_RESULT,
				Result: e.Return,
			},
		}

	case NetworkConnectAttemptTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:    telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_CONNECT_ATTEMPT,
				Sockfd:  e.FD,
				Address: translateNetworkAddress(e.NetworkAddressTelemetryEventData),
			},
		}

	case NetworkConnectResultTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:   telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_CONNECT_RESULT,
				Result: e.Return,
			},
		}

	case NetworkListenAttemptTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:    telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_LISTEN_ATTEMPT,
				Sockfd:  e.FD,
				Backlog: e.Backlog,
			},
		}

	case NetworkListenResultTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:   telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_LISTEN_RESULT,
				Result: e.Return,
			},
		}

	case NetworkRecvfromAttemptTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:   telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_RECVFROM_ATTEMPT,
				Sockfd: e.FD,
			},
		}

	case NetworkRecvfromResultTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:   telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_RECVFROM_RESULT,
				Result: e.Return,
			},
		}

	case NetworkSendtoAttemptTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:    telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_SENDTO_ATTEMPT,
				Sockfd:  e.FD,
				Address: translateNetworkAddress(e.NetworkAddressTelemetryEventData),
			},
		}

	case NetworkSendtoResultTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Network{
			Network: &telemetryAPI.NetworkEvent{
				Type:   telemetryAPI.NetworkEventType_NETWORK_EVENT_TYPE_SENDTO_RESULT,
				Result: e.Return,
			},
		}

	case PerformanceTelemetryEvent:
		values := make([]*telemetryAPI.PerformanceEventValue, len(e.Counters))
		for i, v := range e.Counters {
			var t telemetryAPI.PerformanceEventType
			switch v.EventType {
			case perf.EventTypeHardware:
				t = telemetryAPI.PerformanceEventType_PERFORMANCE_EVENT_TYPE_HARDWARE
			case perf.EventTypeHardwareCache:
				t = telemetryAPI.PerformanceEventType_PERFORMANCE_EVENT_TYPE_HARDWARE_CACHE
			case perf.EventTypeSoftware:
				t = telemetryAPI.PerformanceEventType_PERFORMANCE_EVENT_TYPE_SOFTWARE
			}
			values[i] = &telemetryAPI.PerformanceEventValue{
				Type:   t,
				Config: v.Config,
				Value:  v.Value,
			}
		}
		event.Event = &telemetryAPI.TelemetryEvent_Performance{
			Performance: &telemetryAPI.PerformanceEvent{
				TotalTimeEnabled: e.TotalTimeEnabled,
				TotalTimeRunning: e.TotalTimeRunning,
				Values:           values,
			},
		}

	case LostRecordTelemetryEvent:
		var t telemetryAPI.LostRecordEventType
		switch e.Type {
		case LostRecordTypeUnknown:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_UNKNOWN
		case LostRecordTypeSubscription:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_SUBSCRIPTION
		case LostRecordTypeProcess:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_PROCESS
		case LostRecordTypeContainer:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_CONTAINER
		case LostRecordTypeFileCreate:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_FILE_CREATE
		case LostRecordTypeFileDelete:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_FILE_DELETE
		case LostRecordTypeFileLink:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_FILE_LINK
		case LostRecordTypeFileSymlink:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_FILE_SYMLINK
		case LostRecordTypeFileOpenModify:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_FILE_OPEN_MODIFY
		case LostRecordTypeFileCloseModify:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_FILE_CLOSE_MODIFY
		case LostRecordTypeFileModify:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_FILE_MODIFY
		case LostRecordTypeFileRename:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_FILE_RENAME
		case LostRecordTypeFileAttributeChange:
			t = telemetryAPI.LostRecordEventType_LOST_RECORD_EVENT_TYPE_FILE_ATTRIBUTE_CHANGE
		}
		event.Event = &telemetryAPI.TelemetryEvent_Lost{
			Lost: &telemetryAPI.LostRecordEvent{
				Lost: e.Lost,
				Type: t,
			},
		}

	case ProcessExecTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Process{
			Process: &telemetryAPI.ProcessEvent{
				Type:            telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXEC,
				ExecFilename:    e.Filename,
				ExecCommandLine: e.CommandLine,
				UpdateCwd:       e.CWD,
			},
		}

	case ProcessExitTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Process{
			Process: &telemetryAPI.ProcessEvent{
				Type:           telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_EXIT,
				ExitCode:       e.ExitCode,
				ExitStatus:     e.ExitStatus,
				ExitSignal:     e.ExitSignal,
				ExitCoreDumped: e.ExitCoreDumped,
			},
		}

	case ProcessForkTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Process{
			Process: &telemetryAPI.ProcessEvent{
				Type:           telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_FORK,
				ForkChildId:    e.ChildProcessID,
				ForkChildPid:   e.ChildPID,
				ForkCloneFlags: e.CloneFlags,
				ForkStackStart: e.StackStart,
				UpdateCwd:      e.CWD,
			},
		}

	case ProcessUpdateTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Process{
			Process: &telemetryAPI.ProcessEvent{
				Type:      telemetryAPI.ProcessEventType_PROCESS_EVENT_TYPE_UPDATE,
				UpdateCwd: e.CWD,
			},
		}

	case SyscallEnterTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Syscall{
			Syscall: &telemetryAPI.SyscallEvent{
				Type: telemetryAPI.SyscallEventType_SYSCALL_EVENT_TYPE_ENTER,
				Id:   e.ID,
				Arg0: e.Arguments[0],
				Arg1: e.Arguments[1],
				Arg2: e.Arguments[2],
				Arg3: e.Arguments[3],
				Arg4: e.Arguments[4],
				Arg5: e.Arguments[5],
			},
		}

	case SyscallExitTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Syscall{
			Syscall: &telemetryAPI.SyscallEvent{
				Type: telemetryAPI.SyscallEventType_SYSCALL_EVENT_TYPE_EXIT,
				Id:   e.ID,
				Ret:  e.Return,
			},
		}

	case TickerTelemetryEvent:
		event.Event = &telemetryAPI.TelemetryEvent_Ticker{
			Ticker: &telemetryAPI.TickerEvent{
				Seconds:     e.Seconds,
				Nanoseconds: e.Nanoseconds,
			},
		}

	case UserFunctionCallTelemetryEvent:
		args := make(map[string]*telemetryAPI.UserFunctionCallEvent_FieldValue)
		for k, v := range e.Arguments {
			value := &telemetryAPI.UserFunctionCallEvent_FieldValue{}
			switch v := v.(type) {
			case []byte:
				value.FieldType = telemetryAPI.UserFunctionCallEvent_BYTES
				value.Value = &telemetryAPI.UserFunctionCallEvent_FieldValue_BytesValue{BytesValue: v}
			case string:
				value.FieldType = telemetryAPI.UserFunctionCallEvent_STRING
				value.Value = &telemetryAPI.UserFunctionCallEvent_FieldValue_StringValue{StringValue: v}
			case int8:
				value.FieldType = telemetryAPI.UserFunctionCallEvent_SINT8
				value.Value = &telemetryAPI.UserFunctionCallEvent_FieldValue_SignedValue{SignedValue: int64(v)}
			case int16:
				value.FieldType = telemetryAPI.UserFunctionCallEvent_SINT16
				value.Value = &telemetryAPI.UserFunctionCallEvent_FieldValue_SignedValue{SignedValue: int64(v)}
			case int32:
				value.FieldType = telemetryAPI.UserFunctionCallEvent_SINT32
				value.Value = &telemetryAPI.UserFunctionCallEvent_FieldValue_SignedValue{SignedValue: int64(v)}
			case int64:
				value.FieldType = telemetryAPI.UserFunctionCallEvent_SINT64
				value.Value = &telemetryAPI.UserFunctionCallEvent_FieldValue_SignedValue{SignedValue: v}
			case uint8:
				value.FieldType = telemetryAPI.UserFunctionCallEvent_UINT8
				value.Value = &telemetryAPI.UserFunctionCallEvent_FieldValue_UnsignedValue{UnsignedValue: uint64(v)}
			case uint16:
				value.FieldType = telemetryAPI.UserFunctionCallEvent_UINT16
				value.Value = &telemetryAPI.UserFunctionCallEvent_FieldValue_UnsignedValue{UnsignedValue: uint64(v)}
			case uint32:
				value.FieldType = telemetryAPI.UserFunctionCallEvent_UINT32
				value.Value = &telemetryAPI.UserFunctionCallEvent_FieldValue_UnsignedValue{UnsignedValue: uint64(v)}
			case uint64:
				value.FieldType = telemetryAPI.UserFunctionCallEvent_UINT64
				value.Value = &telemetryAPI.UserFunctionCallEvent_FieldValue_UnsignedValue{UnsignedValue: v}
			}
			args[k] = value
		}
		event.Event = &telemetryAPI.TelemetryEvent_UserCall{
			UserCall: &telemetryAPI.UserFunctionCallEvent{
				Arguments: args,
			},
		}
	}

	return event
}
