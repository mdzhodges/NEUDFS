variable "service_name" {
  description = "Used to name the log group"
  type        = string
}
variable "retention_in_days" {
  description = "How long to keep logs"
  type        = number
  default     = 7
}
