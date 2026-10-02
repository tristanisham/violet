package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"charm.land/log/v2"
	"github.com/google/uuid"
	"github.com/tristanisham/violet/meta"
	"github.com/tristanisham/violet/protocol"
	"github.com/tristanisham/violet/server/ai"
	"github.com/tristanisham/violet/ui/components"
)

const (
	requestQueueSize = 64
	subscriberBuffer = 64
	workerQueueSize  = 16
	chatConcurrency  = 8
	maxChatLanes     = 64
	modelConcurrency = 4
	maxPalettes      = 64
)

type work struct {
	ctx      context.Context
	clientID string
	req      protocol.Request
	legacy   *ai.ChatRequest
}

type chatKey struct{ agent, conversation string }
type chatLane struct {
	pending []work
	running bool
}

type subscription struct {
	ctx    context.Context
	events chan protocol.Event
	done   chan struct{}
}

type engineRuntime struct {
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	store      *ai.Store
	inbox      chan work
	models     chan work
	settings   chan work
	loadModels func(context.Context) ([]protocol.CatalogModel, error)
	runChat    func(context.Context, string, ai.ChatRequest) (json.RawMessage, error)
	graphics   meta.GraphicSettings // Owned exclusively by the settings worker.
	workers    sync.WaitGroup
	subMu      sync.Mutex
	subs       map[string]*subscription
}

func newEngineRuntime(store *ai.Store, graphics meta.GraphicSettings, models func(context.Context) ([]protocol.CatalogModel, error), chat func(context.Context, string, ai.ChatRequest) (json.RawMessage, error)) *engineRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	if models == nil {
		models = loadModels
	}
	if chat == nil {
		chat = runChat
	}
	return &engineRuntime{
		ctx: ctx, cancel: cancel, done: make(chan struct{}), store: store,
		inbox: make(chan work, requestQueueSize), models: make(chan work, workerQueueSize),
		settings: make(chan work, workerQueueSize), graphics: graphics,
		loadModels: models, runChat: chat, subs: make(map[string]*subscription),
	}
}

func (s *Engine) Submit(ctx context.Context, clientID string, req protocol.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := req.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.runtime
	if r == nil || r.ctx.Err() != nil {
		return protocol.ErrStopped
	}
	r.subMu.Lock()
	sub := r.subs[clientID]
	connected := sub != nil && sub.ctx.Err() == nil
	r.subMu.Unlock()
	if !connected {
		return protocol.ErrDisconnected
	}
	// RawMessage is mutable: callers retain ownership of their request buffer.
	req.Data = append(json.RawMessage(nil), req.Data...)
	// ctx only governs admission. Accepted work belongs to the engine and ends
	// with engine shutdown, so local and HTTP clients behave identically even
	// when the caller cancels its submit context right after Submit returns.
	w := work{ctx: context.WithoutCancel(ctx), clientID: clientID, req: req}
	select {
	case r.inbox <- w:
		return nil
	default:
		// Reported once, synchronously; no duplicate error event.
		return protocol.ErrBusy
	}
}

func (s *Engine) Subscribe(ctx context.Context, clientID string) (<-chan protocol.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if clientID == "" {
		return nil, protocol.ErrDisconnected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.runtime
	if r == nil || r.ctx.Err() != nil {
		return nil, protocol.ErrStopped
	}
	r.subMu.Lock()
	defer r.subMu.Unlock()
	// A new subscription replaces an existing one for the same client ID, so a
	// reconnect never races the server noticing the old stream's disconnect.
	r.removeSubscription(clientID)
	sub := &subscription{ctx: ctx, events: make(chan protocol.Event, subscriberBuffer), done: make(chan struct{})}
	r.subs[clientID] = sub
	go func() {
		select {
		case <-ctx.Done():
			r.subMu.Lock()
			if r.subs[clientID] == sub {
				r.removeSubscription(clientID)
			}
			r.subMu.Unlock()
		case <-sub.done:
		}
	}()
	return sub.events, nil
}

func (s *Engine) unsubscribe(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.runtime; r != nil {
		r.subMu.Lock()
		r.removeSubscription(clientID)
		r.subMu.Unlock()
	}
}

// disconnectClients closes every subscription without stopping the engine.
func (s *Engine) disconnectClients() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.runtime; r != nil {
		r.subMu.Lock()
		for id := range r.subs {
			r.removeSubscription(id)
		}
		r.subMu.Unlock()
	}
}

// removeSubscription is called with subMu held, so sends and closes cannot race.
func (r *engineRuntime) removeSubscription(clientID string) {
	if sub := r.subs[clientID]; sub != nil {
		delete(r.subs, clientID)
		close(sub.events)
		close(sub.done)
	}
}

func (r *engineRuntime) emit(w work, kind string, data json.RawMessage, err error, shared bool) {
	event := protocol.Event{
		ID: uuid.NewString(), RequestID: w.req.ID, ClientID: w.clientID,
		OriginClientID: w.clientID, Subject: w.req.Subject, Recipient: w.req.Recipient,
		ConversationID: w.req.ConversationID, Kind: kind,
	}
	if shared {
		event.ClientID = ""
	}
	if err != nil {
		event.Error = err.Error()
	}
	r.subMu.Lock()
	defer r.subMu.Unlock()
	for id, sub := range r.subs {
		if sub.ctx.Err() != nil {
			r.removeSubscription(id)
			continue
		}
		if event.ClientID != "" && event.ClientID != id {
			continue
		}
		event.Data = append(json.RawMessage(nil), data...)
		select {
		case sub.events <- event:
		default:
			// A full channel terminates the stream: consumers must reconnect/resync.
			r.removeSubscription(id)
		}
	}
}

func (r *engineRuntime) fail(w work, err error) {
	if w.legacy != nil {
		log.Error("store legacy chat message", "error", err)
		return
	}
	r.emit(w, "error", nil, err, false)
}

func (r *engineRuntime) run(legacy <-chan Message) {
	defer close(r.done)
	defer r.cancel()
	defer func() {
		r.workers.Wait()
		if err := r.store.Close(); err != nil {
			log.Error("close chat storage", "error", err)
		}
		r.subMu.Lock()
		for id := range r.subs {
			r.removeSubscription(id)
		}
		r.subMu.Unlock()
	}()
	r.workers.Go(func() { r.worker(r.settings, r.handleSettings) })
	for range modelConcurrency {
		r.workers.Go(func() { r.worker(r.models, r.handleModels) })
	}
	lanes := make(map[chatKey]*chatLane)
	completed := make(chan chatKey, chatConcurrency)
	active := 0
	for {
		if r.ctx.Err() != nil {
			return
		}
		select {
		case <-r.ctx.Done():
			return
		case key := <-completed:
			lane := lanes[key]
			lane.running = false
			active--
			if len(lane.pending) == 0 {
				delete(lanes, key)
			}
		case w := <-r.inbox:
			r.route(w, lanes)
		case message, ok := <-legacy:
			if !ok {
				legacy = nil
				continue
			}
			r.routeLegacy(message, lanes)
		}
		active = r.launchChats(lanes, completed, active)
	}
}

func (r *engineRuntime) routeLegacy(message Message, lanes map[chatKey]*chatLane) {
	if message == nil || message.Subject() != protocol.SubjectChat {
		return
	}
	chat, ok := message.Content().(ai.ChatRequest)
	if !ok {
		log.Error("legacy chat message has invalid content")
		return
	}
	chat.Recipient = message.Recipiant()
	chat.Messages = append([]ai.ChatMessage(nil), chat.Messages...)
	r.route(work{ctx: r.ctx, legacy: &chat, req: protocol.Request{
		Subject: protocol.SubjectChat, Recipient: chat.AgentID, ConversationID: chat.ConversationID,
	}}, lanes)
}

func (r *engineRuntime) launchChats(lanes map[chatKey]*chatLane, completed chan<- chatKey, active int) int {
	for key, lane := range lanes {
		if active == chatConcurrency {
			break
		}
		if lane.running || len(lane.pending) == 0 {
			continue
		}
		w := lane.pending[0]
		lane.pending[0] = work{}
		lane.pending = lane.pending[1:]
		lane.running = true
		active++
		r.workers.Go(func() {
			r.execute(w, r.handleChat)
			select {
			case completed <- key:
			case <-r.ctx.Done():
			}
		})
	}
	return active
}

func (r *engineRuntime) route(w work, lanes map[chatKey]*chatLane) {
	if w.req.Subject == protocol.SubjectChat {
		key := chatKey{w.req.Recipient, w.req.ConversationID}
		lane := lanes[key]
		if lane == nil {
			if len(lanes) == maxChatLanes {
				r.fail(w, protocol.ErrBusy)
				return
			}
			lane = &chatLane{}
			lanes[key] = lane
		}
		if len(lane.pending) == workerQueueSize {
			r.fail(w, protocol.ErrBusy)
			return
		}
		lane.pending = append(lane.pending, w)
		return
	}
	queue := r.settings
	if w.req.Subject == protocol.SubjectModels {
		queue = r.models
	}
	select {
	case queue <- w:
	default:
		r.fail(w, protocol.ErrBusy)
	}
}

func (r *engineRuntime) worker(queue <-chan work, handle func(context.Context, work) error) {
	for {
		select {
		case <-r.ctx.Done():
			return
		case w := <-queue:
			r.execute(w, handle)
		}
	}
}

func (r *engineRuntime) execute(w work, handle func(context.Context, work) error) {
	ctx, cancel := context.WithCancel(w.ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	defer cancel()
	if r.ctx.Err() != nil {
		cancel()
	}
	if err := ctx.Err(); err != nil {
		r.fail(w, err)
		return
	}
	if err := handle(ctx, w); err != nil {
		r.fail(w, err)
	}
}

func (r *engineRuntime) handleChat(ctx context.Context, w work) error {
	if w.legacy != nil {
		return r.store.Save(ctx, w.legacy)
	}
	if w.req.ID == "" {
		w.req.ID = uuid.NewString()
	}
	if _, err := uuid.Parse(w.req.ID); err != nil {
		return fmt.Errorf("chat request id must be a UUID")
	}
	var input protocol.ChatInput
	if err := json.Unmarshal(w.req.Data, &input); err != nil {
		return fmt.Errorf("decode chat: %w", err)
	}
	if w.req.Recipient == "" || len(input.Messages) == 0 {
		return fmt.Errorf("chat requires an agent recipient and messages")
	}
	model := input.Model
	if model == "" {
		model = ai.DefaultModel
	}
	chat := ai.ChatRequest{
		Id: uuid.MustParse(w.req.ID), Recipient: model, AgentID: w.req.Recipient,
		ConversationID: w.req.ConversationID, Messages: input.Messages,
	}
	if err := r.store.Create(ctx, &chat); err != nil {
		return fmt.Errorf("save chat: %w", err)
	}
	data, err := json.Marshal(chat)
	if err != nil {
		return err
	}
	r.emit(w, "chat.saved", data, nil, true)
	result, err := r.runChat(ctx, model, chat)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !json.Valid(result) {
		return fmt.Errorf("chat response is not valid JSON")
	}
	r.emit(w, "chat.completed", result, nil, true)
	return nil
}

func (r *engineRuntime) handleModels(ctx context.Context, w work) error {
	models, err := r.loadModels(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(models)
	if err != nil {
		return err
	}
	r.emit(w, "result", data, nil, false)
	return nil
}

func copyGraphics(graphics meta.GraphicSettings) (meta.GraphicSettings, error) {
	if err := graphics.Validate(); err != nil {
		return meta.GraphicSettings{}, err
	}
	data, err := json.Marshal(graphics)
	if err != nil {
		return meta.GraphicSettings{}, err
	}
	var snapshot meta.GraphicSettings
	err = json.Unmarshal(data, &snapshot)
	return snapshot, err
}

func validPaletteName(name string) bool {
	return name != "" && len(name) <= 128 && strings.TrimSpace(name) == name && !strings.ContainsFunc(name, unicode.IsControl)
}

func (r *engineRuntime) handleSettings(ctx context.Context, w work) error {
	next, err := copyGraphics(r.graphics)
	if err != nil {
		return err
	}
	switch w.req.Subject {
	case protocol.SubjectGraphicsGet:
	case protocol.SubjectThemeSelect, protocol.SubjectPaletteCreate:
		var input struct {
			Name    string             `json:"name"`
			Palette components.Palette `json:"palette"`
		}
		if err := json.Unmarshal(w.req.Data, &input); err != nil {
			return fmt.Errorf("decode graphics request: %w", err)
		}
		if !validPaletteName(input.Name) {
			return fmt.Errorf("invalid palette name")
		}
		_, exists := next.Palettes[input.Name]
		if w.req.Subject == protocol.SubjectThemeSelect {
			if !exists {
				return fmt.Errorf("palette %q does not exist", input.Name)
			}
		} else {
			if exists {
				return fmt.Errorf("palette %q already exists", input.Name)
			}
			if len(next.Palettes) >= maxPalettes {
				return fmt.Errorf("palette limit of %d reached", maxPalettes)
			}
			next.Palettes[input.Name] = input.Palette
		}
		next.Palette = input.Name
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := next.Save(); err != nil {
			return fmt.Errorf("save graphic settings: %w", err)
		}
		// Save is atomic; the in-memory snapshot changes only after it succeeds.
		r.graphics = next
	default:
		return fmt.Errorf("unsupported subject %q", w.req.Subject)
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	r.emit(w, "result", data, nil, false)
	return nil
}
