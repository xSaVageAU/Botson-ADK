package telegram

import (
	"context"
	"log"
)

type MockTelegramGateway struct {
	stopChan chan struct{}
}

func NewMockTelegramGateway() *MockTelegramGateway {
	return &MockTelegramGateway{
		stopChan: make(chan struct{}),
	}
}

func (tg *MockTelegramGateway) Name() string {
	return "Telegram"
}

func (tg *MockTelegramGateway) SupportsNativeCommands() bool {
	return false
}

func (tg *MockTelegramGateway) Start(ctx context.Context, runFn func(ctx context.Context, sessionKey string, query string) (string, error)) error {
	log.Println("[Gateway] Telegram gateway active (mock polling started).")
	for {
		select {
		case <-tg.stopChan:
			log.Println("[Gateway] Telegram gateway listener stopped.")
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func (tg *MockTelegramGateway) Stop() error {
	close(tg.stopChan)
	return nil
}
