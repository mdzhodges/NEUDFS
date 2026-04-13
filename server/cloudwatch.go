package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func NewCloudWatchMetrics(cfg aws.Config, environment string) *CloudWatchMetrics {
	cwm := &CloudWatchMetrics{
		client:    cloudwatch.NewFromConfig(cfg),
		namespace: "NEUDFS/gRPC",
		env:       environment,
	}
	go cwm.flushLoop()
	return cwm
}

func (cwm *CloudWatchMetrics) flushLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		cwm.flush()
	}
}

func (cwm *CloudWatchMetrics) flush() {
	cwm.mu.Lock()
	if len(cwm.buffer) == 0 {
		cwm.mu.Unlock()
		return
	}
	batch := cwm.buffer
	cwm.buffer = nil
	cwm.mu.Unlock()

	// CloudWatch accepts max 1000 metrics per PutMetricData call
	for i := 0; i < len(batch); i += 1000 {
		end := i + 1000
		if end > len(batch) {
			end = len(batch)
		}
		_, err := cwm.client.PutMetricData(context.TODO(), &cloudwatch.PutMetricDataInput{
			Namespace:  aws.String(cwm.namespace),
			MetricData: batch[i:end],
		})
		if err != nil {
			fmt.Printf("LOG:\tFailed to publish CloudWatch metrics: %v\n", err)
		}
	}
}

func (cwm *CloudWatchMetrics) Close() {
	cwm.flush()
}

func (cwm *CloudWatchMetrics) Record(method, email, code string, duration time.Duration) {
	now := time.Now()
	dims := []cwtypes.Dimension{
		{Name: aws.String("Method"), Value: aws.String(method)},
		{Name: aws.String("Environment"), Value: aws.String(cwm.env)},
	}
	cwm.mu.Lock()
	defer cwm.mu.Unlock()
	cwm.buffer = append(cwm.buffer,
		cwtypes.MetricDatum{
			MetricName: aws.String("RPCLatency"),
			Value:      aws.Float64(float64(duration.Milliseconds())),
			Unit:       cwtypes.StandardUnitMilliseconds,
			Timestamp:  &now,
			Dimensions: dims,
		},
		cwtypes.MetricDatum{
			MetricName: aws.String("RPCCount"),
			Value:      aws.Float64(1),
			Unit:       cwtypes.StandardUnitCount,
			Timestamp:  &now,
			Dimensions: dims,
		},
	)
	if code != "OK" {
		cwm.buffer = append(cwm.buffer, cwtypes.MetricDatum{
			MetricName: aws.String("RPCErrors"),
			Value:      aws.Float64(1),
			Unit:       cwtypes.StandardUnitCount,
			Timestamp:  &now,
			Dimensions: append(dims, cwtypes.Dimension{
				Name:  aws.String("ErrorCode"),
				Value: aws.String(code),
			}),
		})
	}
}

func logRPCMetric(method, email, code string, duration time.Duration) {
	entry := map[string]any{
		"type":        "rpc_metric",
		"method":      method,
		"email":       email,
		"code":        code,
		"duration_ms": float64(duration.Microseconds()) / 1000.0,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(entry)
	fmt.Println(string(data))
}

func cloudwatchUnaryInterceptor(cwm *CloudWatchMetrics, inner grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := inner(ctx, req, info, handler)
		duration := time.Since(start)
		code := "OK"
		if err != nil {
			if st, ok := status.FromError(err); ok {
				code = st.Code().String()
			}
		}
		email := ""
		if user, ok := ctx.Value("User").(User); ok {
			email = user.Email
		}
		cwm.Record(info.FullMethod, email, code, duration)
		logRPCMetric(info.FullMethod, email, code, duration)

		return resp, err
	}
}

func cloudwatchStreamInterceptor(cwm *CloudWatchMetrics, inner grpc.StreamServerInterceptor) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := inner(srv, ss, info, handler)
		duration := time.Since(start)
		code := "OK"
		if err != nil {
			if st, ok := status.FromError(err); ok {
				code = st.Code().String()
			}
		}
		ctx := ss.Context()
		email := ""
		if user, ok := ctx.Value("User").(User); ok {
			email = user.Email
		}
		cwm.Record(info.FullMethod, email, code, duration)
		logRPCMetric(info.FullMethod, email, code, duration)

		return err
	}
}
