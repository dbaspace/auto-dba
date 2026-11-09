package handler

import "context"

// takeN 取满 n 个结果，或在 ctx 结束时返回已经拿到的。
// 调用方用缓冲通道，超时后仍在探活的协程可以写完而不堵住。
func takeN[T any](ctx context.Context, ch <-chan T, n int) []T {
	if n <= 0 {
		return nil
	}
	out := make([]T, 0, n)
	for len(out) < n {
		select {
		case v := <-ch:
			out = append(out, v)
		case <-ctx.Done():
			return out
		}
	}
	return out
}
