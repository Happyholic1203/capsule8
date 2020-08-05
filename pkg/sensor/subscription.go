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
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Happyholic1203/capsule8/pkg/expression"
	"github.com/Happyholic1203/capsule8/pkg/sys/perf"

	"github.com/golang/glog"
)

// EventSinkDispatchFn is a function that is called to deliver a telemetry
// event for a subscription. This function may be called with a nil event,
// which indicates that new status information is available.
type EventSinkDispatchFn func(event TelemetryEvent)

// eventSinkEnableFn is a function that is called to enable an event sink.
// Note: This functionality currently exists _specifically_ to implement the
// chargen and ticker DEBUG events safely.
type eventSinkEnableFn func()

type eventSinkUnregisterFn func(es *eventSink)

type eventSink struct {
	subscription *Subscription
	eventID      uint64
	enable       eventSinkEnableFn
	unregister   eventSinkUnregisterFn
	filter       *expression.Expression
}

// Subscription contains all of the information about a client subscription
// for telemetry events to be delivered by the sensor.
type Subscription struct {
	sensor          *Sensor
	subscriptionID  uint64
	eventGroupID    int32
	counterGroupIDs []int32
	containerFilter *ContainerFilter
	eventSinks      map[uint64]*eventSink
	dispatchFn      EventSinkDispatchFn

	status     []string
	statusLock sync.Mutex
}

// Run enables and runs a telemetry event subscription. Canceling the specified
// context will cancel the subscription. For each event matching the
// subscription, the specified dispatch function will be called.
func (s *Subscription) Run(
	ctx context.Context,
	dispatchFn EventSinkDispatchFn,
) ([]string, error) {
	status := s.GetStatuses()
	if status != nil {
		for _, st := range status {
			glog.V(1).Infof("Subscription %d: %s",
				s.subscriptionID, st)
		}
	}

	if len(s.eventSinks) == 0 {
		return status, errors.New("Invalid subscription (no filters specified)")
	}
	if dispatchFn != nil {
		s.dispatchFn = dispatchFn
	}

	s.sensor.eventMap.subscribe(s)
	s.sensor.subscriptionMap.insert(s)
	glog.V(2).Infof("Subscription %d registered", s.subscriptionID)

	// Do not return an error after this point!

	go func() {
		<-ctx.Done()
		glog.V(2).Infof("Subscription %d control channel closed",
			s.subscriptionID)

		s.Close()
	}()

	monitor := s.sensor.Monitor()
	if s.eventGroupID != 0 {
		monitor.EnableGroup(s.eventGroupID)
	}
	for _, id := range s.counterGroupIDs {
		monitor.EnableGroup(id)
	}
	for _, es := range s.eventSinks {
		if es.enable != nil {
			es.enable()
		}
	}

	return status, nil
}

// Close disables a running subscription.
func (s *Subscription) Close() {
	monitor := s.sensor.Monitor()
	for _, id := range s.counterGroupIDs {
		monitor.UnregisterEventGroup(id)
	}
	if s.eventGroupID != 0 {
		monitor.UnregisterEventGroup(s.eventGroupID)
	}
	s.sensor.subscriptionMap.remove(s)
	s.sensor.eventMap.unsubscribe(s, nil)
}

// SetContainerFilter sets a container filter to be used for a subscription.
func (s *Subscription) SetContainerFilter(f *ContainerFilter) {
	s.containerFilter = f
}

func (s *Subscription) addEventSink(
	eventID uint64,
	filterExpression *expression.Expression,
) (*eventSink, error) {
	es := &eventSink{
		subscription: s,
		eventID:      eventID,
	}

	if filterExpression != nil {
		// If this is a valid kernel filter, attempt to set it as a
		// kernel filter. If it is either not a valid kernel filter or
		// it fails to set as a kernel filter, set the filter in the
		// sink to fallback to evaluation via the expression package.
		// The err checking code here looks a little weird, but it is
		// what is intended.
		var err error
		if err = filterExpression.ValidateKernelFilter(); err == nil {
			err = s.sensor.Monitor().SetFilter(eventID,
				filterExpression.KernelFilterString())
		}
		if err != nil {
			es.filter = filterExpression
		}
	}

	if s.eventSinks == nil {
		s.eventSinks = make(map[uint64]*eventSink)
	}
	s.eventSinks[eventID] = es
	return es, nil
}

func (s *Subscription) removeEventSink(es *eventSink) {
	delete(s.eventSinks, es.eventID)
}

func (s *Subscription) logStatus(st string) {
	s.statusLock.Lock()
	s.status = append(s.status, st)
	s.statusLock.Unlock()
}

// GetStatuses returns any status information that has been logged since the
// last call to GetStatuses. This function clears the status log.
func (s *Subscription) GetStatuses() []string {
	s.statusLock.Lock()
	statuses := s.status
	s.status = nil
	s.statusLock.Unlock()

	return statuses
}

func (s *Subscription) lostRecordHandler(
	eventid uint64,
	groupid int32,
	sampleID perf.SampleID,
	count uint64,
) {
	glog.V(1).Infof("Lost %d events for eventid %d (group %d)\n",
		count, eventid, groupid)

	var e LostRecordTelemetryEvent
	e.InitWithSampleID(s.sensor, sampleID, count)
	e.Type = LostRecordTypeSubscription
	for _, es := range s.eventSinks {
		es.subscription.dispatchFn(e)
	}

	atomic.AddUint64(&s.sensor.Metrics.KernelSamplesLost, count)
}

func (s *Subscription) createEventGroup() error {
	if s.eventGroupID == 0 {
		monitor := s.sensor.Monitor()
		groupID, err := monitor.RegisterEventGroup("", s.lostRecordHandler)
		if err == nil {
			s.eventGroupID = groupID
		} else {
			s.logStatus(
				fmt.Sprintf("Could not create subscription event group: %v",
					err))
			return err
		}
	}
	return nil
}

func (s *Subscription) registerKprobe(
	address string,
	onReturn bool,
	output string,
	handlerFn perf.TraceEventHandlerFn,
	filterExpr *expression.Expression,
	bindTypes bool,
	options ...perf.RegisterEventOption,
) (*eventSink, error) {
	if err := s.createEventGroup(); err != nil {
		return nil, err
	}

	eventID, err := s.sensor.RegisterKprobe(address, onReturn, output,
		handlerFn, s.eventGroupID, options...)
	if err != nil {
		s.logStatus(
			fmt.Sprintf("Could not register kprobe %s: %v",
				address, err))
		return nil, err
	}

	monitor := s.sensor.Monitor()
	if filterExpr != nil && bindTypes {
		// This is a dynamic kprobe -- determine filterTypes dynamically from
		// the kernel.
		filterTypes := monitor.RegisteredEventFields(eventID)
		err = filterExpr.BindTypes(filterTypes)
		if err != nil {
			s.logStatus(
				fmt.Sprintf("Could not bind kprobe types for %s: %v",
					address, err))
			monitor.UnregisterEvent(eventID)
			return nil, err
		}
	}

	es, err := s.addEventSink(eventID, filterExpr)
	if err != nil {
		s.logStatus(
			fmt.Sprintf("Could not register kprobe %s: %v",
				address, err))
		monitor.UnregisterEvent(eventID)
		return nil, err
	}

	return es, nil
}

func (s *Subscription) registerUprobe(
	executable string,
	address string,
	onReturn bool,
	output string,
	handlerFn perf.TraceEventHandlerFn,
	filterExpr *expression.Expression,
	bindTypes bool,
	options ...perf.RegisterEventOption,
) (*eventSink, error) {
	if err := s.createEventGroup(); err != nil {
		return nil, err
	}

	eventID, err := s.sensor.Monitor().RegisterUprobe(executable, address,
		onReturn, output, handlerFn, s.eventGroupID, options...)
	if err != nil {
		s.logStatus(
			fmt.Sprintf("Could not register uprobe for %s in %s: %v",
				address, executable, err))
		return nil, err
	}

	monitor := s.sensor.Monitor()
	if filterExpr != nil && bindTypes {
		// This is a dynamic kprobe -- determine filterTypes dynamically from
		// the kernel.
		filterTypes := monitor.RegisteredEventFields(eventID)
		err = filterExpr.BindTypes(filterTypes)
		if err != nil {
			s.logStatus(
				fmt.Sprintf("Could not bind uprobe types for %s: %v",
					address, err))
			monitor.UnregisterEvent(eventID)
			return nil, err
		}
	}

	es, err := s.addEventSink(eventID, filterExpr)
	if err != nil {
		s.logStatus(
			fmt.Sprintf("Could not register uprobe for %s in %s: %v",
				address, executable, err))
		monitor.UnregisterEvent(eventID)
		return nil, err
	}

	return es, nil
}

func (s *Subscription) registerTracepoint(
	name string,
	handlerFn perf.TraceEventHandlerFn,
	filterExpr *expression.Expression,
	options ...perf.RegisterEventOption,
) (*eventSink, error) {
	if err := s.createEventGroup(); err != nil {
		return nil, err
	}

	monitor := s.sensor.Monitor()
	eventID, err := monitor.RegisterTracepoint(name, handlerFn,
		s.eventGroupID, options...)
	if err != nil {
		s.logStatus(
			fmt.Sprintf("Could not register tracepoint %s: %v",
				name, err))
		return nil, err
	}

	es, err := s.addEventSink(eventID, filterExpr)
	if err != nil {
		s.logStatus(
			fmt.Sprintf("Could not register tracepoint %s: %v",
				name, err))
		monitor.UnregisterEvent(eventID)
		return nil, err
	}

	return es, nil
}

// DispatchEvent dispatches a telemetry event to the subscription.
func (s *Subscription) DispatchEvent(
	eventID uint64,
	event TelemetryEvent,
	valueGetter expression.FieldValueGetter,
) {
	// Note that filtering here exists solely to implement telemetry events
	// like chargen and ticker. Globally fabricated events like container,
	// file, or task events should be using Sensor.DispatchEvent. Events
	// resulting from the perf subsystem have already been filtered by the
	// EventMonitor code, so should have es.filter == nil and so should
	// also be passing nil for valueGetter.

	if es := s.eventSinks[eventID]; es != nil && es.filter != nil {
		v, err := es.filter.Evaluate(valueGetter)
		if err != nil {
			glog.V(1).Infof("Expression evaluation error: %s", err)
			return
		}
		if !expression.IsValueTrue(v) {
			return
		}
	}

	containerInfo := event.CommonTelemetryEventData().Container
	if !s.containerFilter.Match(containerInfo) {
		return
	}
	s.dispatchFn(event)
}

//
// safeEventSinkMap
// map[uint64]map[int32]*eventSink
//

type eventSinkMap map[uint64]map[int32]*eventSink

func newEventSinkMap(c int) eventSinkMap {
	return make(eventSinkMap, c)
}

type safeEventSinkMap struct {
	sync.Mutex              // used only by writers
	active     atomic.Value // map[uint64]map[int32]*eventSink
}

func newSafeEventSinkMap() *safeEventSinkMap {
	return &safeEventSinkMap{}
}

func (sesm *safeEventSinkMap) getMap() eventSinkMap {
	if value := sesm.active.Load(); value != nil {
		return value.(eventSinkMap)
	}
	return nil
}

func (sesm *safeEventSinkMap) subscribe(subscr *Subscription) {
	sesm.Lock()

	om := sesm.getMap()
	nm := newEventSinkMap(len(om) + len(subscr.eventSinks))

	if om != nil {
		for k, v := range om {
			c := make(map[int32]*eventSink, len(v))
			for k2, v2 := range v {
				c[k2] = v2
			}
			nm[k] = c
		}
	}

	for eventID, es := range subscr.eventSinks {
		eventSinkMap, ok := nm[eventID]
		if !ok {
			eventSinkMap = make(map[int32]*eventSink)
			nm[eventID] = eventSinkMap
		}
		eventSinkMap[subscr.eventGroupID] = es
	}

	sesm.active.Store(nm)
	sesm.Unlock()
}

func (sesm *safeEventSinkMap) unsubscribe(
	subscr *Subscription,
	f func(uint64),
) {
	var (
		deadEventIDs   []uint64
		deadEventSinks []*eventSink
	)

	sesm.Lock()
	if om := sesm.getMap(); om != nil {
		subscriptionID := subscr.eventGroupID
		nm := newEventSinkMap(len(om))
		for eventID, v := range om {
			var m map[int32]*eventSink
			for ID, es := range v {
				if ID != subscriptionID {
					if m == nil {
						m = make(map[int32]*eventSink)
					}
					m[ID] = es
				} else if es.unregister != nil {
					deadEventSinks = append(deadEventSinks,
						es)
				}
			}
			if m == nil {
				deadEventIDs = append(deadEventIDs, eventID)
			} else {
				nm[eventID] = m
			}
		}
		sesm.active.Store(nm)
	}
	sesm.Unlock()

	for _, es := range deadEventSinks {
		es.unregister(es)
	}
	if f != nil {
		for _, eventID := range deadEventIDs {
			f(eventID)
		}
	}
}

//
// safeSubscriptionMap
// map[uint64]*Subscription
//

type subscriptionMap map[uint64]*Subscription

func newSubscriptionMap(c int) subscriptionMap {
	return make(subscriptionMap, c)
}

type safeSubscriptionMap struct {
	sync.Mutex              // used only by writers
	active     atomic.Value // subscriptionMap
}

func newSafeSubscriptionMap() *safeSubscriptionMap {
	return &safeSubscriptionMap{}
}

func (ssm *safeSubscriptionMap) getMap() subscriptionMap {
	if value := ssm.active.Load(); value != nil {
		return value.(subscriptionMap)
	}
	return nil
}

func (ssm *safeSubscriptionMap) insert(s *Subscription) {
	ssm.Lock()

	om := ssm.getMap()
	nm := newSubscriptionMap(len(om) + 1)

	if om != nil {
		for k, v := range om {
			nm[k] = v
		}
	}
	nm[s.subscriptionID] = s

	ssm.active.Store(nm)
	ssm.Unlock()
}

func (ssm *safeSubscriptionMap) remove(s *Subscription) {
	ssm.Lock()
	if om := ssm.getMap(); om != nil {
		nm := newSubscriptionMap(len(om))
		for k, v := range om {
			if v != s {
				nm[k] = v
			}
		}
		ssm.active.Store(nm)
	}
	ssm.Unlock()
}
