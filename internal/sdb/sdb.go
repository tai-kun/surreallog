package sdb

import (
	"errors"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gorilla/websocket"
)

type rpcRequest struct {
	Id     int    `cbor:"id"`
	Method string `cbor:"method"`
	Params any    `cbor:"params"`
}

type rpcError struct {
	Code    int    `cbor:"code"`
	Message string `cbor:"message"`
}

type rpcResponse struct {
	Id     int              `cbor:"id"`
	Error  *rpcError        `cbor:"error"`
	Result *cbor.RawMessage `cbor:"result"`
}

type queryResult struct {
	// Time   string           `cbor:"time"`
	Status string           `cbor:"status"` // "OK" | "ERR"
	Result *cbor.RawMessage `cbor:"result"`
}

// Role: OWNER
type systemAuth struct {
	User string `cbor:"user"`
	Pass string `cbor:"pass"`
}

type serial struct {
	cntr int
	lock sync.Mutex
}

func (s *serial) next() int {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.cntr++
	if s.cntr < 0 {
		s.cntr = 1
	}

	return s.cntr
}

func (s *serial) reset() {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.cntr = 0
}

type SDB struct {
	id        *serial
	ws        *websocket.Conn
	endpoint  string
	CloseErr  error
	CloseChan chan bool
	respChans map[int]chan rpcResponse
	wsLock    sync.Mutex
	respLock  sync.RWMutex
}

func NewSDB() *SDB {
	return &SDB{}
}

func (s *SDB) Connect(endpoint string) error {
	s.wsLock.Lock()
	defer s.wsLock.Unlock()

	if s.ws != nil {
		if s.endpoint == endpoint {
			return nil
		}

		return errors.New(
			"connection conflict between " + s.endpoint + " and " + endpoint,
		)
	}

	dialer := websocket.DefaultDialer
	dialer.EnableCompression = true
	dialer.Subprotocols = append(dialer.Subprotocols, "cbor")
	ws, _, err := dialer.Dial(endpoint, nil)
	if err != nil {
		return err
	}

	if s.id == nil {
		s.id = &serial{}
	}
	s.ws = ws
	s.endpoint = endpoint
	s.CloseErr = nil
	s.CloseChan = make(chan bool)
	s.respChans = make(map[int]chan rpcResponse)
	go s.listen()

	return nil
}

func (s *SDB) Close() error {
	s.wsLock.Lock()

	if s.ws == nil {
		s.wsLock.Unlock()
		return nil
	}

	defer func() {
		s.id.reset()
		s.ws = nil
		s.endpoint = ""
		s.CloseChan = nil
		s.respChans = nil
		s.wsLock.Unlock()
	}()
	close(s.CloseChan)
	errs := make([]error, 0)
	err := s.ws.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	if err != nil {
		errs = append(errs, err)
	}

	if err := s.ws.Close(); err != nil {
		if websocket.IsCloseError(
			err,
			// 正常系
			websocket.CloseNormalClosure, // 1000
			// 早期切断に由来するエラー
			websocket.CloseGoingAway,       // 1001
			websocket.CloseAbnormalClosure, // 1006
		) {
			err = nil
		}

		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (s *SDB) Use(ns, db string) error {
	_, err := s.rpc("use", [2]string{ns, db})

	return err
}

func (s *SDB) Signin(user, pass string) error {
	_, err := s.rpc("signin", [1]systemAuth{{
		User: user,
		Pass: pass,
	}})

	return err
}

func (s *SDB) Query(query string, vars any) (*[]queryResult, error) {
	msg, err := s.rpc("query", [2]any{query, vars})
	if err != nil {
		return nil, err
	}

	var res []queryResult
	if err := cbor.Unmarshal(*msg, &res); err != nil {
		return nil, err
	}

	return &res, nil
}

func (s *SDB) listen() {
	for {
		select {
		case <-s.CloseChan:
			return
		default:
			_, data, err := s.ws.ReadMessage()
			if err != nil {
				switch {
				case errors.Is(err, net.ErrClosed):
					s.CloseErr = err
				default:
					s.CloseErr = err
					<-s.CloseChan
				}
				return
			}

			var resp rpcResponse
			err = cbor.Unmarshal(data, &resp)
			if err != nil {
				log.Println("decode cbor failed:", err)
				continue
			}

			respChan, exists := s.getChan(resp.Id)
			if exists {
				respChan <- resp
			}
		}
	}
}

func (s *SDB) rpc(method string, params any) (*cbor.RawMessage, error) {
	select {
	case <-s.CloseChan:
		return nil, s.CloseErr
	default:
	}

	id := s.id.next()
	respChan, err := s.setChan(id)
	if err != nil {
		return nil, err
	}
	defer s.delChan(id)

	req := rpcRequest{
		Id:     id,
		Method: method,
		Params: params,
	}
	err = s.write(req)
	if err != nil {
		return nil, err
	}

	select {
	case <-time.After(5 * time.Second):
		return nil, errors.New("'" + method + "' rpc timed out after 5 secconds")
	case resp, open := <-respChan:
		if !open {
			return nil, errors.New(
				"'" + method + "' rpc channel(" + strconv.Itoa(id) + ") is closed",
			)
		}
		if resp.Error != nil {
			return nil, errors.New(
				"'" + method + "' rpc (" + strconv.Itoa(id) + ") is failed (" +
					strconv.Itoa(resp.Error.Code) + "): " + resp.Error.Message,
			)
		}

		return resp.Result, nil
	}
}

func (s *SDB) write(req rpcRequest) error {
	s.wsLock.Lock()
	defer s.wsLock.Unlock()

	v, err := cbor.Marshal(req)
	if err != nil {
		return err
	}

	return s.ws.WriteMessage(websocket.BinaryMessage, v)
}

func (s *SDB) setChan(id int) (chan rpcResponse, error) {
	s.respLock.Lock()
	defer s.respLock.Unlock()

	if _, exists := s.respChans[id]; exists {
		return nil, errors.New(
			"rpc request id " + strconv.Itoa(id) + " is in use",
		)
	}

	respChan := make(chan rpcResponse)
	s.respChans[id] = respChan

	return respChan, nil
}

func (s *SDB) getChan(id int) (chan rpcResponse, bool) {
	s.respLock.RLock()
	defer s.respLock.RUnlock()

	respChan, exists := s.respChans[id]

	return respChan, exists
}

func (s *SDB) delChan(id int) {
	s.respLock.Lock()
	defer s.respLock.Unlock()

	delete(s.respChans, id)
}

func At[T any](q *[]queryResult, i int) (*T, error) {
	if i < 0 || i > len(*q)-1 {
		return nil, errors.New("out of range")
	}

	v := (*q)[i]
	if v.Status != "OK" {
		var r string
		if err := cbor.Unmarshal(*v.Result, &r); err != nil {
			return nil, err
		}

		return nil, errors.New(r)
	}

	var t T
	if err := cbor.Unmarshal(*v.Result, &t); err != nil {
		return nil, err
	}

	return &t, nil
}

const cborTagDatetime = 12

func Datetime(t *time.Time) *cbor.Tag {
	if t == nil {
		return nil
	}

	return &cbor.Tag{
		Number:  cborTagDatetime,
		Content: [2]int64{t.Unix(), int64(t.Nanosecond())},
	}
}

const (
	bracketL    = "⟨"
	bracketR    = "⟩"
	bracketEsc  = "\\⟩"
	underscore  = 95 // _
	backtick    = "`"
	backtickEsc = "\\`"
)

func isAsciiDigit(code int) bool {
	return '0' <= code && code <= '9'
}

func isAsciiAlpha(code int) bool {
	return ('A' <= code && code <= 'Z') || ('a' <= code && code <= 'z')
}

func isAsciiAlphaNumeric(code int) bool {
	return isAsciiDigit(code) || isAsciiAlpha(code)
}

func escape(str, left, right, escaped string) string {
	return left + strings.ReplaceAll(str, right, escaped) + right
}

func escapeStartsNumeric(str, left, right, escaped string) string {
	for i := 0; i < len(str); i++ {
		code := int(str[i])

		if i <= 0 && isAsciiDigit(code) {
			return escape(str, left, right, escaped)
		}

		if !(isAsciiAlphaNumeric(code) || code == underscore) {
			return escape(str, left, right, escaped)
		}
	}

	return str
}

func escapeFullNumeric(str, left, right, escaped string) string {
	numeric := true

	for i := 0; i < len(str); i++ {
		code := int(str[i])

		if !(isAsciiAlphaNumeric(code) || code == underscore) {
			return escape(str, left, right, escaped)
		}

		if numeric && !isAsciiDigit(code) {
			numeric = false
		}
	}

	if numeric {
		return escape(str, left, right, escaped)
	}

	return str
}

func QuoteRid(rid string) string {
	if rid == "" {
		return bracketL + bracketR
	}

	return escapeFullNumeric(rid, bracketL, bracketR, bracketEsc)
}

func QuoteIdent(ident string) string {
	if ident == "" {
		return backtick + backtick
	}

	return escapeStartsNumeric(ident, backtick, backtick, backtickEsc)
}
