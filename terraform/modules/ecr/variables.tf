variable "repository_name" {
  description = "The name of the ECR repository"
  type        = string
}

variable "environment" {
  type = string
  description = "Deployment env"
}