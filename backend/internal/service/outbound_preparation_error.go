package service

import "context"

type outboundPreparationErrorKey struct{}

func WithOutboundPreparationError(ctx context.Context, err error) context.Context {
	if err == nil {
		return ctx
	}
	return context.WithValue(ctx, outboundPreparationErrorKey{}, err)
}

func OutboundPreparationError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	err, _ := ctx.Value(outboundPreparationErrorKey{}).(error)
	return err
}
