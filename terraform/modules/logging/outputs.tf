output "log_group_name" {
  description = "The CloudWatch log group name"
  value       = aws_cloudwatch_log_group.this.name
}
