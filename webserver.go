package main

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
)

func index(responseWriter http.ResponseWriter, _ *http.Request) {
	_, _ = responseWriter.Write([]byte("index!"))
}

func listJobs(responseWriter http.ResponseWriter, _ *http.Request) {
	var jobs []Job
	_ = DB.Find(&jobs)

	jobDtos := make([]JobDto, len(jobs))
	for jobNum := 0; jobNum < len(jobs); jobNum++ {
		job := jobs[jobNum]
		jobDtos[jobNum] = JobDto{
			Name:     job.Name,
			Command:  job.Command,
			Type:     job.Type,
			Schedule: job.Schedule,
		}
	}

	jobsJson, _ := json.Marshal(jobDtos)
	_, _ = responseWriter.Write(jobsJson)
}

func getJob(responseWriter http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	jobName := vars["name"]

	var job Job
	DB.Where("name = ?", jobName).Find(&job)

	jobDto := JobDto{
		Name:     job.Name,
		Command:  job.Command,
		Type:     job.Type,
		Schedule: job.Schedule,
	}

	jobJson, _ := json.Marshal(jobDto)
	_, _ = responseWriter.Write(jobJson)
}

func createJob(responseWriter http.ResponseWriter, request *http.Request) {
	var newJob JobDto
	err := json.NewDecoder(request.Body).Decode(&newJob)
	if err != nil {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		return
	}

	result := DB.Create(&Job{
		Name:     newJob.Name,
		Command:  newJob.Command,
		Type:     newJob.Type,
		Schedule: newJob.Schedule,
	})
	if result.Error != nil {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		_, _ = responseWriter.Write([]byte("Job with name " + newJob.Name + " already exists!"))
		return
	}
	JobChannel <- newJob
	responseWriter.WriteHeader(http.StatusCreated)
}

func replaceJob(responseWriter http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	jobName := vars["name"]

	var existingJob Job
	result := DB.Where("name = ?", jobName).Find(&existingJob)
	if result.Error != nil {
		responseWriter.WriteHeader(http.StatusNotFound)
		return
	}

	var replacedJob JobDto
	err := json.NewDecoder(request.Body).Decode(&replacedJob)
	if err != nil {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		return
	}

	if replacedJob.Name != jobName {
		responseWriter.WriteHeader(http.StatusBadRequest)
		_, _ = responseWriter.Write([]byte("Job names in url and body don't match!"))
		return
	}

	existingJob.Command = replacedJob.Command
	existingJob.Type = replacedJob.Type
	existingJob.Schedule = replacedJob.Schedule
	DB.Save(&existingJob)
}

func deleteJob(responseWriter http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	jobName := vars["name"]

	var existingJob Job
	result := DB.Where("name = ?", jobName).Find(&existingJob)
	if result.Error != nil {
		responseWriter.WriteHeader(http.StatusOK)
		return
	}

	DB.Delete(&existingJob)
}

func startJob(responseWriter http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	jobName := vars["name"]

	var job Job
	DB.Where("name = ?", jobName).Find(&job)

	jobDto := JobDto{
		Name:     job.Name,
		Command:  job.Command,
		Type:     job.Type,
		Schedule: job.Schedule,
	}
	JobChannel <- jobDto
}

func stopJob(responseWriter http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	jobName := vars["name"]

	JobCommandChannelMap[jobName] <- JobCommand{
		JobName: jobName,
		Command: Stop,
	}
}

func initWebServer(waitGroup *sync.WaitGroup) {
	router := mux.NewRouter()

	router.HandleFunc("/", index).Methods(http.MethodGet)

	router.HandleFunc("/jobs", listJobs).Methods(http.MethodGet)
	router.HandleFunc("/job/{name}", getJob).Methods(http.MethodGet)
	router.HandleFunc("/job/create", createJob).Methods(http.MethodPost)
	router.HandleFunc("/job/replace/{name}", replaceJob).Methods(http.MethodPut)
	router.HandleFunc("/job/delete/{name}", deleteJob).Methods(http.MethodDelete)

	router.HandleFunc("/job/{name}/start", startJob).Methods(http.MethodGet)
	router.HandleFunc("/job/{name}/stop", stopJob).Methods(http.MethodGet)

	err := http.ListenAndServe(":10000", router)
	if err != nil {
		panic("unable to initialize web server!")
	}

	waitGroup.Done()
}
