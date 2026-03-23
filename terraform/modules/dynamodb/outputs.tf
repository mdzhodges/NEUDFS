output "user_table_name" {
  value = aws_dynamodb_table.user.name
}

output "user_table_arn" {
  value = aws_dynamodb_table.user.arn
}
output "metadata_table_arn" {
  value = aws_dynamodb_table.filedata.arn
}

output "metadata_table_name" {
  value = aws_dynamodb_table.filedata.name

}