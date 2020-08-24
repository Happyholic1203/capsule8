# Getting Telemetry

This document step by step examines code for retrieving different kinds of telemetry events using this repositories internal packages. 

Check out the [command-line-tool example](../examples/command-line-tool) for a faster approach to getting telemetry.

  * [Subscriptions and Filters](#subscipritons-and-filters)
  * [Getting Telemetry](#getting-telemetry-events)
  * [Further Reading](#further-reading)

### Subscriptions and Filters

First let's take a look at the `Subscription` struct defined in `api/v0/subscription.pb.go`:

```go
type Subscription struct {

	EventFilter *EventFilter 

	ContainerFilter *ContainerFilter 

	SinceDuration *google_protobuf1.Int64Value 

	ForDuration *google_protobuf1.Int64Value

	Modifier *Modifier 
}
``` 

The Subscription message identifies a subscriber's interest in telemetry events. The first two fields, EventFilter and ContainerFilter are used to specify what events you want the Capsule8 sensor to return. Let's take a look at the EventFilter definition:

```go
type EventFilter struct {
	SyscallEvents []*SyscallEventFilter

	ProcessEvents []*ProcessEventFilter 

	FileEvents []*FileEventFilter

	KernelEvents []*KernelFunctionCallFilter 

	NetworkEvents []*NetworkEventFilter

	ContainerEvents []*ContainerEventFilter

	ChargenEvents []*ChargenEventFilter 

	TickerEvents []*TickerEventFilter 
}
```

We can see here that EventFilter is actually comprised of slices of more filters that correspond to specific event types. All the specified fields are effectively "ORed" together so we'd get all events that the filters specified. 

For example, let's create an EventFilter for all process lifecycle events, and file open events for filenames that match some regular expression.

To get all process lifecycle events we can use the constants defined in `api/v0` for fork, exec, and exit to create a slice of ProcessEventFilter's:

```go
	processEvents := []*api.ProcessEventFilter{
		&api.ProcessEventFilter{
			Type: api.ProcessEventType_PROCESS_EVENT_TYPE_FORK,
		},
		&api.ProcessEventFilter{
			Type: api.ProcessEventType_PROCESS_EVENT_TYPE_EXEC,
		},
		&api.ProcessEventFilter{
			Type: api.ProcessEventType_PROCESS_EVENT_TYPE_EXIT,
		},
	}
```

Simple enough, how about those file events? Let's say we want to generate an event everytime a file is opened in `/home`

```go
	fileEvents := []*api.FileEventFilter{

		&api.FileEventFilter{
			Type: api.FileEventType_FILE_EVENT_TYPE_OPEN,

			FilenamePattern: &wrappers.StringValue{
				Value: "/home/*",
			},
		},
	}
```

Again, here we're making use of the constants defined in `api/v0`, checking out that package is the simplest way to see the capabilities of the Capsule8 sensor. 

Let's create our final subscription object:

```go
	eventFilter := &api.EventFilter{
		ProcessEvents:   processEvents,
		FileEvents:      fileEvents,
	}

	ourSubscription := &api.Subscription{
		EventFilter: eventFilter,
	}

```

And that's all it takes to specify what events we want. In the following section we'll see how to start the Capsule8 sensor, submit our subscription, and start receiving events!

### Getting Telemetry Events

Checkout the [Quickstart]() document for building and starting the Capsule8 sensor on your system.

The Capsule8 sensor can be connected to via tcp or a unix socket, you'll want this custom dial function to plug into the grpc connection for using both from your client:

```go
// Custom gRPC Dialer that understands "unix:/path/to/sock" as well as TCP addrs
func dialer(addr string, timeout time.Duration) (net.Conn, error) {
	var network, address string

	parts := strings.Split(addr, ":")
	if len(parts) > 1 && parts[0] == "unix" {
		network = "unix"
		address = parts[1]
	} else {
		network = "tcp"
		address = addr
	}

	return net.DialTimeout(network, address, timeout)
}
```

Now that we have that let's take a look at how we'd open up a stream of events using our subscription.

We make a connection to the unix socket our Capsule8 sensor is running on, using the custom dialer above to do so. Let's also not worry about TLS for now.

```go
       conn, err := grpc.Dial("unix:/var/run/capsule8/sensor.sock",  
	    grpc.WithDialer(dialer),   
            grpc.WithInsecure())       
```


We create a client to our telemetry service that we'll be able to pass our subscription into using the now established connection:
```go
    c := api.NewTelemetryServiceClient(conn)   
	if err != nil {
		fmt.Fprintf(os.Stderr, "grpc.Dial: %s\n", err)
		os.Exit(1)
    }
```

And now it's as simple as calling client.GetEvents, passing our subscription in the request:
```go
    stream, err := c.GetEvents(context.Background(), &api.GetEventsRequest{ 
		Subscription: ourSubscription,
	})

```

Finally let's create a simple loop that receives events and prints them out as they come in! 

```go
	for {
		ev, err := stream.Recv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Recv: %s\n", err)
			os.Exit(1)
		}

		for _, e := range ev.Events {
			fmt.Println(e)
		}
	}
```

Here's that all put together followed by the output we expect to see when run:

```go 
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"

	api "github.com/Happyholic1203/capsule8/api/v0"
	"github.com/golang/protobuf/ptypes/wrappers"
	"google.golang.org/grpc"
)

func main() {

	processEvents := []*api.ProcessEventFilter{
		&api.ProcessEventFilter{
			Type: api.ProcessEventType_PROCESS_EVENT_TYPE_FORK,
		},
		&api.ProcessEventFilter{
			Type: api.ProcessEventType_PROCESS_EVENT_TYPE_EXEC,
		},
		&api.ProcessEventFilter{
			Type: api.ProcessEventType_PROCESS_EVENT_TYPE_EXIT,
		},
	}

	fileEvents := []*api.FileEventFilter{

		&api.FileEventFilter{
			Type: api.FileEventType_FILE_EVENT_TYPE_OPEN,

			FilenamePattern: &wrappers.StringValue{
				Value: "/home/*",
			},
		},
	}

	eventFilter := &api.EventFilter{
		ProcessEvents: processEvents,
		FileEvents:    fileEvents,
	}

	ourSubscription := &api.Subscription{
		EventFilter: eventFilter,
	}

	// Create telemetry service client
	conn, err := grpc.Dial("unix:/var/run/capsule8/sensor.sock",
		grpc.WithDialer(dialer),
		grpc.WithInsecure())

	c := api.NewTelemetryServiceClient(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grpc.Dial: %s\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := c.GetEvents(ctx, &api.GetEventsRequest{
		Subscription: ourSubscription,
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "GetEvents: %s\n", err)
		os.Exit(1)
	}

	// Exit cleanly on Control-C
	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt)

	go func() {
		<-signals
		cancel()
	}()

	for {
		ev, err := stream.Recv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Recv: %s\n", err)
			os.Exit(1)
		}

		for _, e := range ev.Events {
			fmt.Println(e)
		}
	}
}

// Custom gRPC Dialer that understands "unix:/path/to/sock" as well as TCP addrs
func dialer(addr string, timeout time.Duration) (net.Conn, error) {
	var network, address string

	parts := strings.Split(addr, ":")
	if len(parts) > 1 && parts[0] == "unix" {
		network = "unix"
		address = parts[1]
	} else {
		network = "tcp"
		address = addr
	}

	return net.DialTimeout(network, address, timeout)
}



```


And when we build and run this:

```
[*] sudo ./telemetry 
event:<id:"ab264238af4aaa14ee1e434303d32f9352f66282a3a82bfbea0d75fc592b220d" process_id:"0a491a31fb9931f8490d823cd32bed2c37c8b031883169a02bdb84d7604dedcb" process_pid:32484 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:30 sensor_monotime_nanos:1518797458470228099 file:<type:FILE_EVENT_TYPE_OPEN filename:"/home/grant/.cache/google-chrome/Default/Cache/37586f4b9464a393_0" open_flags:32962 open_mode:384 > cpu:2 credentials:<uid:1000 gid:1000 euid:1000 egid:1000 suid:1000 sgid:1000 fsuid:1000 fsgid:1000 > process_tgid:6901 > 
event:<id:"e82b7b5e8a63cd8b2d4b6d845bce330469fa7bf4832f65ec56de83a8a699a817" process_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" process_pid:1524 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:31 sensor_monotime_nanos:1518797459510814794 process:<type:PROCESS_EVENT_TYPE_EXIT > cpu:3 process_tgid:3947 > 
event:<id:"616455a45d0d297177d296e1c4c98727bcb5079b337558edc9451c992597506d" process_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" process_pid:3947 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:32 sensor_monotime_nanos:1518797459510964023 process:<type:PROCESS_EVENT_TYPE_FORK fork_child_pid:1579 fork_child_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" > cpu:3 credentials:<uid:1000 gid:1000 euid:1000 egid:1000 suid:1000 sgid:1000 fsuid:1000 fsgid:1000 > process_tgid:3947 > 
event:<id:"f9ffaa6ddf366a176fdefff5469fa0535f80c42c4a115ba45b7919d5db29acfe" process_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" process_pid:3947 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:33 sensor_monotime_nanos:1518797459510990443 process:<type:PROCESS_EVENT_TYPE_FORK fork_child_pid:1583 fork_child_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" > cpu:3 credentials:<uid:1000 gid:1000 euid:1000 egid:1000 suid:1000 sgid:1000 fsuid:1000 fsgid:1000 > process_tgid:3947 > 
event:<id:"3df82a1c11686252af67b5ef9e5284e05cc33c534736ce5264a1a9b3b7c5d939" process_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" process_pid:3947 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:34 sensor_monotime_nanos:1518797459511014318 process:<type:PROCESS_EVENT_TYPE_FORK fork_child_pid:1584 fork_child_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" > cpu:3 credentials:<uid:1000 gid:1000 euid:1000 egid:1000 suid:1000 sgid:1000 fsuid:1000 fsgid:1000 > process_tgid:3947 > 
event:<id:"441c8a04b7ef3d5746fadd3911bd9d6bf1bafe4bdf9e7ec4dc17e7fb5526d5ca" process_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" process_pid:3947 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:35 sensor_monotime_nanos:1518797459511187123 process:<type:PROCESS_EVENT_TYPE_FORK fork_child_pid:1585 fork_child_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" > cpu:3 credentials:<uid:1000 gid:1000 euid:1000 egid:1000 suid:1000 sgid:1000 fsuid:1000 fsgid:1000 > process_tgid:3947 > 
event:<id:"cea9dca9776708c2cd1631cbba0bb1c65586fe95bd35e602d23ef1b6160689d9" process_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" process_pid:3947 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:36 sensor_monotime_nanos:1518797459511488807 process:<type:PROCESS_EVENT_TYPE_FORK fork_child_pid:1591 fork_child_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" > cpu:3 credentials:<uid:1000 gid:1000 euid:1000 egid:1000 suid:1000 sgid:1000 fsuid:1000 fsgid:1000 > process_tgid:3947 > 
event:<id:"d28fa5ce88aca07b5eb90f7acacdbc2fe95b4d6a1d20e75ff5a482e092f3a640" process_id:"6bb31bec439111412597717095f65c90ad8ecd90167ff5619ebc7009a44c9d69" process_pid:1378 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:37 sensor_monotime_nanos:1518797459604136712 process:<type:PROCESS_EVENT_TYPE_EXIT > cpu:1 process_tgid:2156 > 
event:<id:"73659894da7cbba888a099653d525a1911c47283421e9a0cef7e762fd73fbc50" process_id:"0a491a31fb9931f8490d823cd32bed2c37c8b031883169a02bdb84d7604dedcb" process_pid:32484 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:38 sensor_monotime_nanos:1518797459629586027 file:<type:FILE_EVENT_TYPE_OPEN filename:"/home/grant/.config/google-chrome/.com.google.Chrome.Hf3LJ1" open_flags:32962 open_mode:384 > credentials:<uid:1000 gid:1000 euid:1000 egid:1000 suid:1000 sgid:1000 fsuid:1000 fsgid:1000 > process_tgid:6901 > 
event:<id:"3bd8841406a3ef83997265440580ef4ea1e24790ae59c857f00efa4ebc3403e8" process_id:"0a491a31fb9931f8490d823cd32bed2c37c8b031883169a02bdb84d7604dedcb" process_pid:32484 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:39 sensor_monotime_nanos:1518797459629762888 file:<type:FILE_EVENT_TYPE_OPEN filename:"/home/grant/.config/google-chrome/.com.google.Chrome.Hf3LJ1" open_flags:32769 > credentials:<uid:1000 gid:1000 euid:1000 egid:1000 suid:1000 sgid:1000 fsuid:1000 fsgid:1000 > process_tgid:6901 > 
event:<id:"d88daaca82b61491258792f45643cbe92ecb8aec0b78c211ae9b770beaddf43e" process_id:"0a491a31fb9931f8490d823cd32bed2c37c8b031883169a02bdb84d7604dedcb" process_pid:32484 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:40 sensor_monotime_nanos:1518797459641530099 file:<type:FILE_EVENT_TYPE_OPEN filename:"/home/grant/.config/google-chrome/Default/.com.google.Chrome.getF3i" open_flags:32962 open_mode:384 > credentials:<uid:1000 gid:1000 euid:1000 egid:1000 suid:1000 sgid:1000 fsuid:1000 fsgid:1000 > process_tgid:6901 > 
event:<id:"c369851b8883096d84da52e2626dd51efabe46b7258fb24ce0e9de1fc74f0f64" process_id:"0a491a31fb9931f8490d823cd32bed2c37c8b031883169a02bdb84d7604dedcb" process_pid:32484 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:41 sensor_monotime_nanos:1518797459641622085 file:<type:FILE_EVENT_TYPE_OPEN filename:"/home/grant/.config/google-chrome/Default/.com.google.Chrome.getF3i" open_flags:32769 > credentials:<uid:1000 gid:1000 euid:1000 egid:1000 suid:1000 sgid:1000 fsuid:1000 fsgid:1000 > process_tgid:6901 > 
event:<id:"6f6d7e00e2374e697ee8944f07bff5529914d1bc25c4d71de3e3d83cc7655974" process_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" process_pid:1585 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:42 sensor_monotime_nanos:1518797460148327323 process:<type:PROCESS_EVENT_TYPE_EXIT > cpu:3 process_tgid:3947 > 
event:<id:"334189d03da6ebe2b91c2aed5c580d921e413df9993f20689dec72fcd32b9b24" process_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" process_pid:1579 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:43 sensor_monotime_nanos:1518797460148431175 process:<type:PROCESS_EVENT_TYPE_EXIT > cpu:1 process_tgid:3947 > 
event:<id:"2b30f6bc0028fd3aed19e0edd6c9704530434de1ccbf33fae6273277562d4f1e" process_id:"77581652d50b2571cf0ed2c48c73b320cc127013cdd7463f07c250117a8ea2f2" process_pid:971 sensor_id:"7335d9bbc69527304b48281fdb3b031f8ca1305bb9081a44b93cc8179e5d9d49" sensor_sequence_number:44 sensor_monotime_nanos:1518797460148476213 process:<type:PROCESS_EVENT_TYPE_EXIT > cpu:3 process_tgid:3947 > 
^C Recv: rpc error: code = Canceled desc = context canceled
```

Looks like I have chrome running...

### Further Reading

- For more examples see the [examples](examples) directory
- For help running the Capsule8 sensor checkout the [Quickstart](docs/Quickstart.md) 
- For ideas of how to utilitize the features of the Capsule8 sensor, you may be interested in reading about [auditd](http://security.blogoverflow.com/2013/01/a-brief-introduction-to-auditd/) 