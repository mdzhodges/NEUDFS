output "user_table_name" {
  value = module.dynamodb.user_table_name
}

output "user_table_arn" {
  value = module.dynamodb.user_table_arn
}

output "metadata_table_name" {
  value = module.dynamodb.metadata_table_name
}

output "metadata_table_arn" {
  value = module.dynamodb.metadata_table_arn
}

output "ecr_repository_url" {
  value = module.ecr.repository_url
}

output "log_group_name" {
  value = module.logging.log_group_name
}

output "s3_bucket_id" {
  value = module.s3_storage.bucket_id
}

output "s3_bucket_arn" {
  value = module.s3_storage.bucket_arn
}

output "server_address" {
  value       = "${module.nlb.dns_name}:50051"
  description = "gRPC server address — use this in the client"
}