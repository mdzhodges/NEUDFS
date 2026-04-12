resource "aws_s3_bucket" "neudfs_bucket" {
  bucket = "neudfs-storage-${var.environment}-matt"
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

data "aws_iam_policy_document" "bucket_access" {
  statement {
    sid     = "AllowEcsTaskRoleList"
    effect  = "Allow"
    actions = ["s3:ListBucket"]

    resources = [aws_s3_bucket.neudfs_bucket.arn]

    principals {
      type        = "AWS"
      identifiers = [var.lab_role_arn]
    }
  }

  statement {
    sid    = "AllowEcsTaskRoleObjects"
    effect = "Allow"
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
    ]

    resources = ["${aws_s3_bucket.neudfs_bucket.arn}/*"]

    principals {
      type        = "AWS"
      identifiers = [var.lab_role_arn]
    }
  }
}

resource "aws_s3_bucket_policy" "neudfs_bucket_policy" {
  bucket = aws_s3_bucket.neudfs_bucket.id
  policy = data.aws_iam_policy_document.bucket_access.json
}
