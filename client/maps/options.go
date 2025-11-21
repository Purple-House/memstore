package maps

import "time"

type options struct {
	Address string
	APIKey  string
	Timeout time.Duration
}

func defaultOptions() options {
	return options{
		Timeout: 5 * time.Second,
	}
}

type Option func(*options)

func WithAddress(addr string) Option {
	return func(o *options) { o.Address = addr }
}

func WithAPIKey(key string) Option {
	return func(o *options) { o.APIKey = key }
}

func WithTimeout(t time.Duration) Option {
	return func(o *options) { o.Timeout = t }
}
