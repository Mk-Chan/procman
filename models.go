package main

import (
	"github.com/jinzhu/gorm"
)

type JobType string

const (
	Continuous JobType = "continuous"
	OneTime            = "one_time"
)

type Job struct {
	gorm.Model
	Name     string `gorm:"unique;not null"`
	Command  string
	Type     JobType
	Schedule string
}
