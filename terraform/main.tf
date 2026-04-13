terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    docker = {
      source  = "kreuzwerker/docker"
      version = "~> 3.0"
    }
  }

}

provider "aws" {
  region = var.aws_region
}
data "aws_ecr_authorization_token" "registry" {}

# Configure Docker provider to authenticate against ECR automatically
provider "docker" {
  registry_auth {
    address  = data.aws_ecr_authorization_token.registry.proxy_endpoint
    username = data.aws_ecr_authorization_token.registry.user_name
    password = data.aws_ecr_authorization_token.registry.password
  }
}

data "aws_caller_identity" "current" {}

# Default VPC and subnets for ECS
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

module "dynamodb" {
  source      = "./modules/dynamodb"
  table_name  = "user"
  environment = var.environment
}

module "s3_storage" {
  source      = "./modules/s3_storage"
  environment = var.environment
  lab_role_arn = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/LabRole"
}

module "ecr" {
  source          = "./modules/ecr"
  repository_name = var.ecr_repository_name
  environment     = var.environment
}

module "logging" {
  source       = "./modules/logging"
  service_name = var.service_name
}

module "nlb" {
  source = "./modules/nlb"

  app_name       = var.service_name
  environment    = var.environment
  vpc_id         = data.aws_vpc.default.id
  subnet_ids     = data.aws_subnets.default.ids
  container_port = 50051
}

module "ecs" {
  source = "./modules/ecs"
  container_port = 50051
  image = "${module.ecr.repository_url}:latest"

  app_name    = var.service_name
  environment = var.environment
  aws_region  = var.aws_region

  lab_role_arn = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/LabRole"

  ecr_repository_url = module.ecr.repository_url

  vpc_id     = data.aws_vpc.default.id
  subnet_ids = data.aws_subnets.default.ids

  log_group_name      = module.logging.log_group_name
  user_table_name     = module.dynamodb.user_table_name
  metadata_table_name = module.dynamodb.metadata_table_name
  s3_bucket_name      = module.s3_storage.bucket_id

  target_group_arn = module.nlb.target_group_arn
  s3_bucket_name = module.s3_storage.bucket_name
}

resource "docker_image" "server" {
  # Use the URL from the ecr module, and tag it "latest"
  name = "${module.ecr.repository_url}:latest"

  build {
    context    = "${path.module}/.."
    dockerfile = "server/Dockerfile"
  }
}

resource "docker_registry_image" "server" {
  # this will push :latest → ECR
  name = docker_image.server.name
}
