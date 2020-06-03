# ProcMan
A simple process control solution. The intended use case is for this application to be started by a native init-system
such as systemd or crontab and then used for further process management itself.

Each process registered with ProcMan is called a `job` and is simply a shell command that will be executed using a
`system()` call. Most likely `sh -c <command>` on a common GNU/Linux distribution.

Metadata about jobs is stored in an sqlite3 database.

### Concepts
A `job` is a process configuration that has a `type` will be executed with some `schedule`. There are 2 `type`s of jobs:
1. OneTime `one_time` - Will run once on-schedule/on-demand and wait till next actuation.
2. Continuous `continuous` - Will run continuously, restarting on failure with backoff once begun.

Currently, only a single schedule is supported: `reboot`. This will simply start the job when ProcMan runs. Any other
value is ignored, and the job will need to be started manually.

Job stdout is redirected to a local folder `logs/<jobname>/<unix-timestamp-of-runtime>.log`. A job can be found in one
of several states at any time during its lifetime (inspired by supervisord):
```
Stopped
Starting
Running
Retrying
Error - not yet implemented
Stopping
Exited
Unknown - not yet implemented
```

### Example
* Start ProcMan:
```
./procman 10000
```
* Create a simple job that echos "hello":
```
curl --request POST \
  --url http://localhost:10000/job/create \
  --header 'content-type: application/json' \
  --data '{
	"name": "test",
	"command": "echo hello",
	"type": "one_time",
	"schedule": "reboot"
}'
```
That should create a `logs/test/<unix-timestamp>.log` file with `hello` written inside. The job we created ran
automatically because we set a `reboot` schedule.
* Try to run the job again:
```
curl http://localhost:10000/job/test/start
```
That will once again create a new log file with the same content as the last.
* Similarly, a running job can be stopped with:
```
curl http://localhost:10000/job/test/stop
```
This is also the only way to stop a `continuous` job once it has been started barring failure.
The entire API is provided below.

### API
```
Job CRUD operations
===================
/jobs - GET to list all jobs
/job/{name} - GET to retrieve job metadata
/job/create - POST to add a new job
/job/replace/{name} - PUT to replace job
/job/delete/{name} - DELETE to disable job

Job State Control & Query
=========================
/job/{name}/start - GET to start job
/job/{name}/stop - GET to stop job
/job/{name}/restart - GET to restart job
/job/{name}/state - GET to retrieve job state
```

### Setup
Requires Go 1.14 to build.
1. Clone the repository `git clone https://github.com/Mk-Chan/procman`
2. Build the code from the repository `go build`
3. Start the app `./procman <webserver-port>`
