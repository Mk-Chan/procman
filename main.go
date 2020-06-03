package main

import (
	"sync"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
)

var DB *gorm.DB
var JobChannel chan JobDto
var JobDataMap map[string]*JobData

func main() {
	DB, _ = gorm.Open("sqlite3", "procman.db")
	defer DB.Close()
	JobChannel = make(chan JobDto)
	defer close(JobChannel)
	JobDataMap = make(map[string]*JobData)

	DB.AutoMigrate(&Job{})

	var waitGroup sync.WaitGroup

	waitGroup.Add(1)
	go jobListener(JobChannel, &waitGroup)

	waitGroup.Add(1)
	go initWebServer(&waitGroup)

	var jobs []Job
	_ = DB.Find(&jobs)

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
