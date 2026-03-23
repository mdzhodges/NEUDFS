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
