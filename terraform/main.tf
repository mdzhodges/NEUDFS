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

module "dynamodb" {
  source       = "./modules/dynamodb"
  table_name   = "user"
  environment  = var.environment
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
