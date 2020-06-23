package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log"
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

func executeJob(ctx context.Context, job JobDto, logFilePath string) error {
	commandSplit := strings.Split(job.Command, " ")
	command := exec.Command(commandSplit[0], commandSplit[1:]...)
	processDoneChannel := make(chan struct{})

	log.Println("[JOB]", "[INFO]", "starting job", job.Name)
	procOut, _ := command.StdoutPipe()
	err := command.Start()
	if err != nil {
		log.Println("[JOB]", "[ERROR]", "failed to start job", job.Name, ":", err)
		return errors.New("failed to start job " + job.Name)
	}

	go trackProcess(procOut, logFilePath)
	go func() {
		_ = command.Wait()
		processDoneChannel <- struct{}{}
	}()

	JobDataMap[job.Name].State = Running
	log.Println("[JOB]", "[INFO]", job.Name, "is running")

	select {
	case <-processDoneChannel:
		log.Println("[JOB]", "[INFO]", job.Name, "finished")
		return nil
	case <-ctx.Done():
		log.Println("[JOB]", "[INFO]", "stopping", job.Name)
		_ = command.Process.Signal(syscall.SIGTERM)
		<-processDoneChannel
		log.Println("[JOB]", "[INFO]", job.Name, "stopped")
		return nil
	}
}

func continuousJob(ctx context.Context, job JobDto) {
	jobExecutor := func() error {
		timestampStr := strconv.FormatInt(time.Now().UnixNano(), 10)
		logFilePath := filepath.Join("logs", job.Name, timestampStr) + ".log"
		return executeJob(ctx, job, logFilePath)
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
		case <-ctx.Done():
			return
		}
	}
}

func oneTimeJob(ctx context.Context, job JobDto) {
	timestampStr := strconv.FormatInt(time.Now().UnixNano(), 10)
	logFilePath := filepath.Join("logs", job.Name, timestampStr) + ".log"
	err := executeJob(ctx, job, logFilePath)
	if err != nil {
		panic("failed to execute job!")
	}
}

func runJob(ctx context.Context, job JobDto, waitGroup *sync.WaitGroup) {
	waitTillAverageCpuUsage(30)

	if job.Type == Continuous {
		continuousJob(ctx, job)
	} else if job.Type == OneTime {
		oneTimeJob(ctx, job)
	} else {
		panic("unknown job type!")
	}

	JobDataMap[job.Name].State = Stopped

	waitGroup.Done()
}

func initJobManager(jobData *JobData, waitGroup *sync.WaitGroup) {
	backgroundCtx := context.Background()
	jobName := jobData.Dto.Name

	logDirName := filepath.Join("logs", jobName)
	err := os.MkdirAll(logDirName, os.ModePerm)
	if err != nil {
		panic("failed to create log folder!")
	}

	JobDataMap[jobName] = jobData
	defer delete(JobDataMap, jobName)

	log.Println("[INIT]", "initialized job manager:", jobName)

	commandChannel := jobData.CommandChannel
	if jobData.Dto.Schedule == "reboot" {
		go func() {
			commandChannel <- JobCommand{
				JobName: jobName,
				Command: Start,
			}
		}()
	}

	var ctx context.Context
	var cancel context.CancelFunc
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

			JobDataMap[jobName].Dto = JobDto{
				Name:     existingJob.Name,
				Command:  existingJob.Command,
				Type:     existingJob.Type,
				Schedule: existingJob.Schedule,
			}

			jobWaitGroup.Add(1)
			ctx, cancel = context.WithCancel(backgroundCtx)
			go runJob(ctx, jobData.Dto, &jobWaitGroup)
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

			JobDataMap[jobName].Dto = JobDto{
				Name:     existingJob.Name,
				Command:  existingJob.Command,
				Type:     existingJob.Type,
				Schedule: existingJob.Schedule,
			}

			jobWaitGroup.Add(1)
			ctx, cancel = context.WithCancel(backgroundCtx)
			go runJob(ctx, jobData.Dto, &jobWaitGroup)
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
	log.Println("[INIT]", "initialized job listener")
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
