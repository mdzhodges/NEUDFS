output "bucket_id" {
  description = "The name of the bucket"
  value       = aws_s3_bucket.neudfs_bucket.id
}

output "bucket_arn" {
  description = "The ARN of the bucket for IAM policies"
  value       = aws_s3_bucket.neudfs_bucket.arn
}