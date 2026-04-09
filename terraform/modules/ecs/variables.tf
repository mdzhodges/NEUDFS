variable "app_name" {
  type        = string
  description = "Application name"
}

variable "environment" {
  type        = string
  description = "Deployment environment"
}

variable "aws_region" {
  type        = string
  description = "AWS region"
  default     = "us-east-1"
}

# IAM - using lab role since we can't create our own
variable "lab_role_arn" {
  type        = string
  description = "ARN of the lab role to use for task execution and task role"
}

# Container config
variable "ecr_repository_url" {
  type        = string
  description = "ECR repository URL for the container image"
}

variable "container_port" {
  type        = number
  description = "Port the container listens on"
  default     = 8080
}

variable "cpu" {
  type        = number
  description = "Fargate task CPU units"
  default     = 256
}

variable "memory" {
  type        = number
  description = "Fargate task memory in MB"
  default     = 512
}

variable "desired_count" {
  type        = number
  description = "Number of tasks to run"
  default     = 1
}

# Networking
variable "vpc_id" {
  type        = string
  description = "VPC ID for the ECS service"
}

variable "subnet_ids" {
  type        = list(string)
  description = "Subnet IDs for the ECS tasks"
}

# From other modules
variable "log_group_name" {
  type        = string
  description = "CloudWatch log group name"
}

variable "user_table_name" {
  type        = string
  description = "DynamoDB user table name"
}

variable "metadata_table_name" {
  type        = string
  description = "DynamoDB metadata table name"
}

variable "target_group_arn" {
  type        = string
  description = "ARN of the NLB target group to register ECS tasks with"
}

variable "min_capacity" {
  default = 2
}
variable "max_capacity" {
  default = 6
}
variable "s3_bucket_name" {
  type=string
  default = "neudfs-storage-dev"
}