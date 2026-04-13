variable "environment" {
  description = "The deployment environment (e.g., dev, prod)"
  type        = string
}

variable "lab_role_arn" {
  description = "IAM role ARN used by ECS tasks (granted access via bucket policy)"
  type        = string
}
