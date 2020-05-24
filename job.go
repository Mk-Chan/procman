package main

import (
	"bufio"
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
	Status                 = "status"
)

type JobCommand struct {
	JobName string
	Command JobCommandType
}

func executeJob(job JobDto, logFileName string) error {
	commandSplit := strings.Split(job.Command, " ")
	command := exec.Command(commandSplit[0], commandSplit[1:]...)
	procOut, _ := command.StdoutPipe()

	filePtr, err := os.Create(logFileName)
	if err != nil {
		panic("unable to create/truncate file " + logFileName)
	}
	defer filePtr.Close()

	_ = command.Start()
	proc := command.Process

	go func() {
		scanner := bufio.NewScanner(procOut)
		for scanner.Scan() {
			outputLine := scanner.Text()
			_, err = filePtr.WriteString(outputLine + "\n")
			if err != nil {
				panic("unable to write to file " + logFileName)
			}
		}
	}()

	go func() {
		commandChannel := JobCommandChannelMap[job.Name]
		for jobCmd := range commandChannel {
			if jobCmd.Command == Stop {
				_ = proc.Signal(syscall.SIGTERM)
			}
		}
	}()

	_ = command.Wait()
	return nil
}

func continuousJob(job JobDto, waitGroup *sync.WaitGroup) {
	jobExecutor := func() error {
		timestampStr := strconv.FormatInt(time.Now().UnixNano(), 10)
		logFileName := filepath.Join("logs", job.Name, timestampStr) + ".log"
		return executeJob(job, logFileName)
	}

	for true {
		var existingJob Job
		result := DB.Where("name = ?", job.Name).Find(&existingJob)
		if result.Error != nil {
			break
		}
		err := backoff.Retry(jobExecutor, backoff.NewExponentialBackOff())
		if err != nil {
			panic("failed to backoff!")
			return
		}
	}

	waitGroup.Done()
}

func oneTimeJob(job JobDto, waitGroup *sync.WaitGroup) {
	timestampStr := strconv.FormatInt(time.Now().UnixNano(), 10)
	logFileName := filepath.Join("logs", job.Name, timestampStr) + ".log"
	err := executeJob(job, logFileName)
	if err != nil {
		panic("failed to execute job!")
	}

	waitGroup.Done()
}

func runJob(job JobDto, waitGroup *sync.WaitGroup) {
	waitTillAverageCpuUsage(30)

	jobCommandChannel := make(chan JobCommand)
	JobCommandChannelMap[job.Name] = jobCommandChannel

	logDirName := filepath.Join("logs", job.Name)
	err := os.MkdirAll(logDirName, os.ModePerm)
	if err != nil {
		panic("failed to create log folder!")
	}

	if job.Type == Continuous {
		go continuousJob(job, waitGroup)
	} else if job.Type == OneTime {
		go oneTimeJob(job, waitGroup)
	} else {
		panic("unknown job type!")
	}
}

func jobListener(jobChannel <-chan JobDto, waitGroup *sync.WaitGroup) {
	for job := range jobChannel {
		waitGroup.Add(1)
		runJob(job, waitGroup)
	}

	waitGroup.Done()
}
