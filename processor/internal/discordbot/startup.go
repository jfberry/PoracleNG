package discordbot

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

const gatewayRetryInterval = time.Minute

// openWithRetry covers the initial connection: discordgo only reconnects
// automatically after a session has opened successfully. Return a fully
// initialized bot even when the caller runs this connection in the background.
func openWithRetry(ctx context.Context, open func() error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := open()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil || !retryableGatewayError(err) {
			return err
		}

		log.Warnf("Discord gateway failed to connect: %v; retrying in %s", err, gatewayRetryInterval)
		timer := time.NewTimer(gatewayRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func retryableGatewayError(err error) bool {
	if errors.Is(err, discordgo.ErrUnauthorized) ||
		errors.Is(err, discordgo.ErrWSShardBounds) ||
		errors.Is(err, discordgo.ErrWSAlreadyOpen) {
		return false
	}
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Response != nil {
		switch restErr.Response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return false
		}
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		// Discord marks these gateway close codes as non-reconnectable:
		// https://docs.discord.com/developers/topics/opcodes-and-status-codes#gateway-close-event-codes
		switch closeErr.Code {
		case 4004, 4010, 4011, 4012, 4013, 4014:
			return false
		}
	}
	return true
}
