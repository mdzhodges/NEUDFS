variable "app_name" {
  type        = string
  description = "Application name"
}

variable "environment" {
  type        = string
  description = "Deployment environment"
}

variable "vpc_id" {
  type        = string
  description = "VPC ID"
}

variable "subnet_ids" {
  type        = list(string)
  description = "Public subnet IDs for the NLB"
}

variable "container_port" {
  type        = number
  description = "Port the container (and NLB listener) uses"
  default     = 8080
}
