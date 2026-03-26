resource "aws_lb" "nlb" {
  name               = "${var.app_name}-nlb-${var.environment}"
  internal           = false
  load_balancer_type = "network"
  subnets            = var.subnet_ids

  tags = {
    Name        = "${var.app_name}-nlb"
    Environment = var.environment
  }
}

resource "aws_lb_target_group" "grpc" {
  name        = "${var.app_name}-tg-${var.environment}"
  port        = var.container_port
  protocol    = "TCP"
  vpc_id      = var.vpc_id
  target_type = "ip"

  health_check {
    protocol            = "TCP"
    port                = var.container_port
    healthy_threshold   = 2
    unhealthy_threshold = 2
    interval            = 30
  }

  tags = {
    Name        = "${var.app_name}-tg"
    Environment = var.environment
  }
}

resource "aws_lb_listener" "grpc" {
  load_balancer_arn = aws_lb.nlb.arn
  port              = var.container_port
  protocol          = "TCP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.grpc.arn
  }
}
