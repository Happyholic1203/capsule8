# Subscriptions-CLI

This is a simple command line utility for subscribing to telemetry events from the [capsule8 sensor](https://github.com/Happyholic1203/capsule8/)

The telemtry cli takes one argument: a path to a subscription file. Subscriptions are a high level object used by clients to the telemetry service
in the sensor for receiving telemetry events. These files are simply subscription structs marshalled to JSON. The subscriptions directory
contains a few examples. Also check out the [doc for getting telemetry](../../docs/Getting-Telemetry.md) for an in-depth view of how this works.

If you write a subscription file that you think others would find useful, feel free to contribute it!

# Quickstart

1) [Build and run the capsule8 sensor](../../README.md#Quickstart)

2) Build this example tool by running `make`

3) Run the tool by passing a JSON subscription file located in [subscriptions](./subscriptions) (*You will need to run this as root*)

```
$ sudo ./c8cli subscriptions/ContainerEvents.json 
{"event":{"id":"8ad3dd7a1a5806aa0f17e2462fee78763a95ae864b7b299596b360ef9836fb56","container_id":"64ed276626f484c5855311b273f83b351fdcad114192dc71ae4078e67fc2583d","sensor_id":"589ec097c0dd8528da31afcc088f2dadef2d6b19907c576543bbf8fd638b9fa0","sensor_sequence_number":22,"sensor_monotime_nanos":1518972566976649118,"Event":{"Container":{"type":1,"image_id":"422dc563ca3260ad9ef5c47a1c246f5065d7f177ce51f4dd208efd82967ff182","image_name":"fedora"}},"cpu":2}}
{"event":{"id":"aac070f40a42f1164814b1c421962df01afb9a3ce5a53208dd8c8db1864ded31","container_id":"64ed276626f484c5855311b273f83b351fdcad114192dc71ae4078e67fc2583d","sensor_id":"589ec097c0dd8528da31afcc088f2dadef2d6b19907c576543bbf8fd638b9fa0","sensor_sequence_number":24,"sensor_monotime_nanos":1518972567223518433,"Event":{"Container":{"type":2,"image_id":"422dc563ca3260ad9ef5c47a1c246f5065d7f177ce51f4dd208efd82967ff182","image_name":"fedora","host_pid":29209}},"cpu":2}}
{"event":{"id":"c7d3616a71bec48ccc061292ba663eb221e0035238e47276bcdddd70a34e326a","container_id":"64ed276626f484c5855311b273f83b351fdcad114192dc71ae4078e67fc2583d","sensor_id":"589ec097c0dd8528da31afcc088f2dadef2d6b19907c576543bbf8fd638b9fa0","sensor_sequence_number":25,"sensor_monotime_nanos":1518972574338632237,"Event":{"Container":{"type":3,"image_id":"422dc563ca3260ad9ef5c47a1c246f5065d7f177ce51f4dd208efd82967ff182","image_name":"fedora"}},"cpu":2}}
```
