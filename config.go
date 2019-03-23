package main

import (
	corev1 "k8s.io/api/core/v1"
)

type Config struct {
	PHPIpam struct {
		Host     string
		AppID    string `yaml:"appId"`
		Username string
		Password string
	}
	Subnets []struct {
		CIDR      string `yaml:"cidr"`
		Namespace *string
		Type      *corev1.ServiceType
		Regex     *string
	} `yaml:"subnets"`
}
