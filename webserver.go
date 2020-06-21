package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

type JobStateResponse struct {
	JobName string    `json:"job_name"`
	State   string    `json:"state"`
	Time    time.Time `json:"time"`
}

type JobStatesResponse struct {
	JobStates []struct {
		JobName string `json:"job_name"`
		State   string `json:"state"`
	} `json:"jobs"`
	Time time.Time `json:"time"`
}

type ErrorResponse struct {
	Message string `json:"message"`
}

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

func listJobStates(responseWriter http.ResponseWriter, _ *http.Request) {
	var jobStatesResponse JobStatesResponse
	jobStatesResponse.Time = time.Now()
	for jobName := range JobDataMap {
		jobDto := JobDataMap[jobName]
		jobStatesResponse.JobStates = append(jobStatesResponse.JobStates, struct {
			JobName string `json:"job_name"`
			State   string `json:"state"`
		}{
			JobName: jobName,
			State:   string(jobDto.State),
		})
	}
	_ = json.NewEncoder(responseWriter).Encode(jobStatesResponse)
}

func getJob(responseWriter http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	jobName := vars["name"]

	var job Job
	result := DB.Where("name = ?", jobName).Find(&job)
	if result.Error != nil {
		responseWriter.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(responseWriter).Encode(ErrorResponse{Message: "Job " + jobName + " not found"})
		return
	}

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
		_ = json.NewEncoder(responseWriter).Encode(ErrorResponse{Message: "Unable to parse request body!"})
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
		_ = json.NewEncoder(responseWriter).Encode(ErrorResponse{Message: "Job with name " + newJob.Name + " already exists!"})
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
		_ = json.NewEncoder(responseWriter).Encode(ErrorResponse{Message: "Job names in url and body don't match!"})
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
		return
	}

	DB.Delete(&existingJob)
}

func jobStart(responseWriter http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	jobName := vars["name"]

	var job Job
	DB.Where("name = ?", jobName).Find(&job)

	jobData, ok := JobDataMap[jobName]
	if ok && jobData != nil {
		jobData.CommandChannel <- JobCommand{
			JobName: jobName,
			Command: Start,
		}
	} else {
		responseWriter.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(responseWriter).Encode(ErrorResponse{Message: "Job " + jobName + " not found"})
	}
}

func jobStop(responseWriter http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	jobName := vars["name"]

	jobData, ok := JobDataMap[jobName]
	if ok && jobData != nil {
		jobData.CommandChannel <- JobCommand{
			JobName: jobName,
			Command: Stop,
		}
	} else {
		responseWriter.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(responseWriter).Encode(ErrorResponse{Message: "Job " + jobName + " not found"})
	}
}

func jobRestart(responseWriter http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	jobName := vars["name"]

	var job Job
	DB.Where("name = ?", jobName).Find(&job)

	jobData, ok := JobDataMap[jobName]
	if ok && jobData != nil {
		jobData.CommandChannel <- JobCommand{
			JobName: jobName,
			Command: Restart,
		}
	} else {
		responseWriter.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(responseWriter).Encode(ErrorResponse{Message: "Job " + jobName + " not found"})
	}
}

func jobState(responseWriter http.ResponseWriter, request *http.Request) {
	vars := mux.Vars(request)
	jobName := vars["name"]

	jobData, ok := JobDataMap[jobName]
	if ok && jobData != nil {
		_ = json.NewEncoder(responseWriter).Encode(JobStateResponse{
			JobName: jobName,
			State:   string(jobData.State),
			Time:    time.Now(),
		})
	} else {
		responseWriter.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(responseWriter).Encode(ErrorResponse{Message: "Job " + jobName + " not found"})
	}
}

func initWebServer(port int, waitGroup *sync.WaitGroup) {
	router := mux.NewRouter()
	router.Use(loggingMiddleware)
	// router.Use(profilingMiddleware)

	router.HandleFunc("/", index).Methods(http.MethodGet)

	router.HandleFunc("/jobs", listJobs).Methods(http.MethodGet)
	router.HandleFunc("/jobs/states", listJobStates).Methods(http.MethodGet)

	router.HandleFunc("/job/{name}", getJob).Methods(http.MethodGet)
	router.HandleFunc("/job/create", createJob).Methods(http.MethodPost)
	router.HandleFunc("/job/replace/{name}", replaceJob).Methods(http.MethodPut)
	router.HandleFunc("/job/delete/{name}", deleteJob).Methods(http.MethodDelete)

	router.HandleFunc("/job/{name}/start", jobStart).Methods(http.MethodGet)
	router.HandleFunc("/job/{name}/stop", jobStop).Methods(http.MethodGet)
	router.HandleFunc("/job/{name}/restart", jobRestart).Methods(http.MethodGet)
	router.HandleFunc("/job/{name}/state", jobState).Methods(http.MethodGet)

	log.Println("[INIT]", "initialized web server")
	listenAddress := ":" + strconv.Itoa(port)
	err := http.ListenAndServe(listenAddress, router)
	if err != nil {
		panic("unable to initialize web server!")
	}

	waitGroup.Done()
}
