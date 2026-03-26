terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

}

provider "aws" {
  region = var.aws_region
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
}

module "ecs" {
  source = "./modules/ecs"

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

  target_group_arn = module.nlb.target_group_arn
}