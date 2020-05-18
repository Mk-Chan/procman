package main

import (
	"sync"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

var DB *gorm.DB
var JobChannel chan JobDto
var JobCommandChannelMap map[string]chan JobCommand

func main() {
	DB, _ = gorm.Open("sqlite3", "procman.db")
	defer DB.Close()
	JobChannel = make(chan JobDto)
	defer close(JobChannel)
	JobCommandChannelMap = make(map[string]chan JobCommand)
	defer func() {
		for jobName := range JobCommandChannelMap {
			close(JobCommandChannelMap[jobName])
		}
	}()

	DB.AutoMigrate(&Job{})

	var waitGroup sync.WaitGroup

	waitGroup.Add(1)
	go jobListener(JobChannel, &waitGroup)

	waitGroup.Add(1)
	go initWebServer(&waitGroup)

	var jobs []Job
	_ = DB.Where("schedule = ?", "reboot").Find(&jobs)

	for jobNum := 0; jobNum < len(jobs); jobNum++ {
		job := &jobs[jobNum]
		jobDto := JobDto{
			Name:     job.Name,
			Command:  job.Command,
			Type:     job.Type,
			Schedule: job.Schedule,
		}
		JobChannel <- jobDto
	}

	waitGroup.Wait()
}
