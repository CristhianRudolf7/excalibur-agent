package daemon

import (
	"context"
	"errors"

	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/voice"
)

var _ gateway.VoiceLiveBackend = (*DaemonBackend)(nil)

func (b *DaemonBackend) liveVoiceClient() *voice.Client {
	if b == nil || b.Services == nil || b.Services.Voice == nil {
		return nil
	}
	return b.Services.Voice.Client
}

// VoiceListen adapts the attached voice client without persisting audio or text.
func (b *DaemonBackend) VoiceListen(ctx context.Context, language string) (gateway.VoiceListenSession, error) {
	client := b.liveVoiceClient()
	if client == nil {
		return nil, gateway.ErrVoiceUnavailable
	}
	session, err := client.Listen(ctx, language)
	if err != nil {
		return nil, liveVoiceError(err)
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &daemonVoiceListen{ListenSession: session, events: make(chan gateway.VoiceListenEvent, 32), cancel: cancel, done: make(chan struct{})}
	safego.Go("daemon-voice-listen", nil, func() {
		defer close(s.done)
		defer close(s.events)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-session.Events():
				if !ok {
					return
				}
				out := gateway.VoiceListenEvent{Type: event.Type, Item: event.Item, Text: event.Text, Code: event.Code}
				select {
				case s.events <- out:
				case <-ctx.Done():
					return
				}
			}
		}
	})
	return s, nil
}

type daemonVoiceListen struct {
	*voice.ListenSession
	events chan gateway.VoiceListenEvent
	cancel context.CancelFunc
	done   chan struct{}
}

func (s *daemonVoiceListen) Append(pcm []byte) error {
	return liveVoiceError(s.ListenSession.Append(pcm))
}
func (s *daemonVoiceListen) Commit() error                           { return liveVoiceError(s.ListenSession.Commit()) }
func (s *daemonVoiceListen) Clear() error                            { return liveVoiceError(s.ListenSession.Clear()) }
func (s *daemonVoiceListen) Events() <-chan gateway.VoiceListenEvent { return s.events }
func (s *daemonVoiceListen) Close() error {
	s.cancel()
	err := s.ListenSession.Close()
	<-s.done
	return err
}

// VoiceSpeak streams only to the requesting socket through the gateway callback.
func (b *DaemonBackend) VoiceSpeak(ctx context.Context, text string, write func([]byte) error) error {
	client := b.liveVoiceClient()
	if client == nil {
		return gateway.ErrVoiceUnavailable
	}
	return liveVoiceError(client.SpeakStream(ctx, text, write))
}

func liveVoiceError(err error) error {
	if errors.Is(err, voice.ErrUnavailable) {
		return gateway.ErrVoiceUnavailable
	}
	return err
}
