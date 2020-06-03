package main

import (
	"bufio"
	context2 "context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cenkalti/backoff"
)

type JobDto struct {
	Name     string  `json:"name"`
	Command  string  `json:"command"`
	Type     JobType `json:"type"`
	Schedule string  `json:"schedule"`
}

type JobCommandType string

const (
	Start   JobCommandType = "start"
	Stop                   = "stop"
	Restart                = "restart"
)

type JobCommand struct {
	JobName string
	Command JobCommandType
}

type JobState string

const ( // TODO: Implement all these
	Stopped  JobState = "stopped"
	Starting          = "starting"
	Running           = "running"
	Retrying          = "retrying"
	Error             = "error"
	Stopping          = "stopping"
	Exited            = "exited"
	Unknown           = "unknown"
)

type JobData struct {
	Dto            JobDto
	State          JobState
	CommandChannel chan JobCommand
}

func trackProcess(procOut io.ReadCloser, logFilePath string) {
	scanner := bufio.NewScanner(procOut)

	filePtr, err := os.Create(logFilePath)
	if err != nil {
		panic("unable to create/truncate file " + logFilePath)
	}
	defer filePtr.Close()

	for scanner.Scan() {
		outputLine := scanner.Text()
		_, err = filePtr.WriteString(outputLine + "\n")
		if err != nil {
			panic("unable to write to file " + logFilePath)
		}
	}
}

func executeJob(context context2.Context, job JobDto, logFilePath string) error {
	commandSplit := strings.Split(job.Command, " ")
	command := exec.Command(commandSplit[0], commandSplit[1:]...)

	procOut, _ := command.StdoutPipe()
	_ = command.Start()
	go trackProcess(procOut, logFilePath)

	JobDataMap[job.Name].State = Running

	processDoneChannel := make(chan struct{})
	go func() {
		_ = command.Wait()
		processDoneChannel <- struct{}{}
	}()

	select {
	case <-processDoneChannel:
		return nil
	case <-context.Done():
		_ = command.Process.Signal(syscall.SIGTERM)
		<-processDoneChannel
		return nil
	}
}

func continuousJob(context context2.Context, job JobDto) {
	jobExecutor := func() error {
		timestampStr := strconv.FormatInt(time.Now().UnixNano(), 10)
		logFilePath := filepath.Join("logs", job.Name, timestampStr) + ".log"
		return executeJob(context, job, logFilePath)
	}

	for true {
		var existingJob Job
		result := DB.Where("name = ?", job.Name).Find(&existingJob)
		if result.Error != nil {
			JobDataMap[job.Name].State = Exited
			break
		}
		err := backoff.Retry(jobExecutor, func() *backoff.ExponentialBackOff {
			JobDataMap[job.Name].State = Retrying
			return backoff.NewExponentialBackOff()
		}())
		if err != nil {
			panic("failed to backoff!")
			return
		}

		select {
		case <-context.Done():
			break
		}
	}
}

func oneTimeJob(context context2.Context, job JobDto) {
	timestampStr := strconv.FormatInt(time.Now().UnixNano(), 10)
	logFilePath := filepath.Join("logs", job.Name, timestampStr) + ".log"
	err := executeJob(context, job, logFilePath)
	if err != nil {
		panic("failed to execute job!")
	}
}

func runJob(context context2.Context, job JobDto, waitGroup *sync.WaitGroup) {
	waitTillAverageCpuUsage(30)

	if job.Type == Continuous {
		continuousJob(context, job)
	} else if job.Type == OneTime {
		oneTimeJob(context, job)
	} else {
		panic("unknown job type!")
	}

	JobDataMap[job.Name].State = Stopped

	waitGroup.Done()
}

func initJobManager(jobData *JobData, waitGroup *sync.WaitGroup) {
	backgroundContext := context2.Background()
	jobName := jobData.Dto.Name

	logDirName := filepath.Join("logs", jobName)
	err := os.MkdirAll(logDirName, os.ModePerm)
	if err != nil {
		panic("failed to create log folder!")
	}

	JobDataMap[jobName] = jobData
	defer delete(JobDataMap, jobName)

	commandChannel := jobData.CommandChannel
	var context context2.Context
	var cancel context2.CancelFunc
	var jobWaitGroup sync.WaitGroup

	for jobCmd := range commandChannel {
		if jobCmd.Command == Start {
			jobData.State = Starting

			var existingJob Job
			result := DB.Where("name = ?", jobCmd.JobName).Find(&existingJob)
			if result.Error != nil {
				jobData.State = Exited
				break
			}

			jobWaitGroup.Add(1)
			context, cancel = context2.WithCancel(backgroundContext)
			go runJob(context, jobData.Dto, &jobWaitGroup)
		} else if jobCmd.Command == Restart {
			jobData.State = Stopping

			if cancel != nil {
				cancel()
				jobWaitGroup.Wait()
			}

			jobData.State = Starting

			var existingJob Job
			result := DB.Where("name = ?", jobCmd.JobName).Find(&existingJob)
			if result.Error != nil {
				jobData.State = Exited
				break
			}

			jobWaitGroup.Add(1)
			context, cancel = context2.WithCancel(backgroundContext)
			go runJob(context, jobData.Dto, &jobWaitGroup)
		} else if jobCmd.Command == Stop {
			jobData.State = Stopping

			if cancel != nil {
				cancel()
				jobWaitGroup.Wait()
			}

			jobData.State = Stopped
		}
	}

	waitGroup.Done()
}

func jobListener(jobChannel <-chan JobDto, waitGroup *sync.WaitGroup) {
	for job := range jobChannel {
		jobData := JobData{
			Dto:            job,
			State:          Stopped,
			CommandChannel: make(chan JobCommand),
		}
		waitGroup.Add(1)
		go initJobManager(&jobData, waitGroup)
	}

	waitGroup.Done()
}
