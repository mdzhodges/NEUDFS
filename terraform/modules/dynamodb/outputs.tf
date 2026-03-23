output "table_name" {
  value = aws_dynamodb_table.shopping_cart.name
}

output "table_arn" {
  value = aws_dynamodb_table.shopping_cart.arn
}