# Hacking on Capsule8

## Local CI

Capsule8 uses CircleCI 2.0 for continuous integration testing. The CI
build job can be run locally using CircleCI's CLI container with the
`ci` make target:

```
$ make ci
docker run -it --rm \
	-e DOCKER_API_VERSION= \
	-v /var/run/docker.sock:/var/run/docker.sock \
	-v /home/dino/src/github.com/Happyholic1203/capsule8:/home/dino/src/github.com/Happyholic1203/capsule8 \
	--workdir /home/dino/src/github.com/Happyholic1203/capsule8 \
	circleci/picard \
	circleci build
====>> Spin up Environment
Build-agent version 0.0.4606-189b121 (2017-12-22T21:13:51+0000)
Starting container circleci/golang
  using image circleci/golang@sha256:dad9d639804f824af8d12b01bf5b8cd7fceb60e2bbc6a288e2161894e9b7fe0a
[...]
```

## Sensor

### Logging and Tracing

Capsule8 uses [glog](https://github.com/golang/glog) for logging and
all executables accept the `-v` and `-vmodule` flags. In general, log
levels 1-9 increase verbosity of logging while >10 increase verbosity
of event and request-specific tracing. These log levels may be set
globally with a command-line argument like `-v=9` or on a specific
file with `-vmodule=process_info=11`.

### Functional Tests

The functional tests expect to be able to communicate with a running
Capsule8 Sensor and run Docker containers to monitor. The easiest way
to run the functional tests is by using the `Makefile` targets that
run the Sensor and functional tests within Docker containers.

First, run the Capsule8 Sensor Docker container detached:

```
$ make run_sensor_detach
[...]
docker run --privileged -d --name capsule8-sensor --rm -v /proc:/var/run/capsule8/proc/:ro -v /sys/kernel/debug:/sys/kernel/debug -v /sys/fs/cgroup:/sys/fs/cgroup -v /var/lib/docker:/var/lib/docker:ro -v /var/run/capsule8:/var/run/capsule8 -v /var/run/docker:/var/run/docker:ro sha256:e312365e4838c439b9d032413ff2cd9ad61c45ecd7a22e356b9b857e88e8bdfd
5245331f0008f54095ae2b64f0f24cf44a9509f4cc0c9b1e826e72c835f789a9
```

```
$ make run_functional_test
docker run -v /var/run/capsule8:/var/run/capsule8 -v /var/run/docker.sock:/var/run/docker.sock sha256:bc5369c69a73ecfddc01f3d7e1a26578966149fdbaed7af879e44e8aaf1d7843 
PASS
```

You can now stop the detached `capsule8-sensor` container by name:
```
$ docker stop capsule8-sensor
```

#### Running Functional Tests on a Remote Host

The recommended way to run both the Sensor and the functional tests on
a remote host is by setting the `DOCKER_HOST` environment variable to
the appropriate Docker socket.

```
$ make DOCKER_HOST=tcp://127.0.0.1:2375 run_sensor_detached
[...]
$ make DOCKER_HOST=tcp://127.0.0.1:2375 run_functional_test
[...]
```

#### Debugging Functional Tests

The functional test container logs verbose output to
`/var/lib/capsule8/log/` by default and these logs can be copied out
with `docker cp`:

```
$ docker cp 51fd59662c80:/var/lib/capsule8/log /tmp/51fd59662c80
$ ls /tmp/51fd59662c80/
functional.test.51fd59662c80.unknownuser.log.INFO.20171224-221923.1  functional.test.INFO@
```

Additional flags can be specified to the test runner by setting the
`TEST_FLAGS` Makefile variable:

```
$ make TEST_FLAGS="-test.v -test.parallel 1 -test.run Crash -v=10" run_functional_test
[...]
=== RUN   TestCrash
=== RUN   TestCrash/buildContainer
=== RUN   TestCrash/runTelemetryTest
--- PASS: TestCrash (1.93s)
    --- PASS: TestCrash/buildContainer (0.16s)
    --- PASS: TestCrash/runTelemetryTest (1.77s)
PASS
```

### Performance Tests

In order to measure performance improvements or regressions of the
Sensor, there is a simple macro benchmark in `test/benchmark`. The
benchmark assumes that only one Docker container is running at a time,
so make sure to perform this testing when no other Docker containers
are running.

Start the benchmark in one window, as shown below. It will print out
the number of events received on the subscription as well as the
`getrusage(2)` delta between when a container starts and stops:

```
$ cd test/benchmark
$ go build .
$ sudo ./benchmark 
fa29d62433bf493a2c494a0cb2ff90aa2372b4b00f47cfc0903c55a67b7479ed Events:73606 avg_user_us_per_event:249 avg_sys_us_per_event:105 {Events:73606 Subscriptions:1} {Utime:{Sec:18 Usec:389000} Stime:{Sec:7 Usec:767000} Maxrss:16544 Ixrss:0 Idrss:0 Isrss:0 Minflt:1055 Majflt:0 Nswap:0 Inblock:0 Oublock:8 Msgsnd:0 Msgrcv:0 Nsignals:0 Nvcsw:613202 Nivcsw:43554}
```

In order to generate a large number of events, you can use the kernel
compile container in `test/benchmark/kernel_compile`:

```
$ cd test/benchmark/kernel_compile
$ make
[...]
840.79user 64.07system 2:11.97elapsed 685%CPU (0avgtext+0avgdata 146052maxresident)k
0inputs+28248outputs (0major+24220864minor)pagefaults 0swaps
```
