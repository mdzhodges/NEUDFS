variable "aws_region" {
  type        = string
  description = "AWS region"
  default     = "us-east-1"
}

variable "environment" {
  type        = string
  description = "Deployment environment"
  default     = "dev"
}

variable "service_name" {
  type        = string
  description = "Name of the service"
  default     = "neudfs"
}

variable "ecr_repository_name" {
  type        = string
  description = "Name of the ECR repository"
  default     = "neudfs"
}
