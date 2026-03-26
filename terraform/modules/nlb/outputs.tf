output "dns_name" {
  value       = aws_lb.nlb.dns_name
  description = "DNS name of the NLB — use this as the server address in the client"
}

output "target_group_arn" {
  value       = aws_lb_target_group.grpc.arn
  description = "ARN of the target group to attach ECS tasks to"
}
