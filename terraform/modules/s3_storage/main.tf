resource "aws_s3_bucket" "neudfs_bucket" {
  bucket = "neudfs-storage-${var.environment}"
  force_destroy = true
  
  tags = {
    Name        = "NEUDFS Storage"
    Environment = var.environment
  }
}

resource "aws_s3_bucket_public_access_block" "neudfs_block" {
  bucket = aws_s3_bucket.neudfs_bucket.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# provides durability be versioning our bucket
resource "aws_s3_bucket_versioning" "versioning" {
  bucket = aws_s3_bucket.neudfs_bucket.id
  versioning_configuration {
    status = "Enabled"
  }
}