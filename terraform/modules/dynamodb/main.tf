resource "aws_dynamodb_table" "user"{
  name = "user"
  billing_mode = "PAY_PER_REQUEST"
  hash_key = "email"
  attribute {
    name = "email"
    type = "S"
  }
  tags = {
    Name = "user table"
    Environment = var.environment
  }
}

resource "aws_dynamodb_table" "filedata" {
  name           = "classroom_metadata"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "pk"
  range_key = "sk"

  attribute {
    name = "pk"
    type = "S"
  }
  attribute {
    name = "sk"
    type = "S"
  }

  attribute {
    name = "owner"
    type = "S"
  }

  attribute {
    name = "last_modified"
    type = "S"
  }

  #for querying user ids across classrooms
  global_secondary_index {
    name            = "owner-index"
    hash_key        = "owner"
    range_key       = "last_modified"
    projection_type = "ALL"
  }

  tags = {
    Name        = "classroom_metadata"
    Environment = var.environment
  }
}