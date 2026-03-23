# Create (or ensure) an ECR repo exists
resource "aws_ecr_repository" "this" {
  name = var.repository_name
  # You can add lifecycle_policy, scan_on_push, etc., here
}
