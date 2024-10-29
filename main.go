package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/fxamacker/cbor/v2"
	"github.com/tai-kun/surreallog/internal/ghc"
	"github.com/tai-kun/surreallog/internal/sdb"
)

const envPrefix = "SURREALLOG_"

func getCommand() (string, []string, error) {
	if len(os.Args) < 2 {
		msg := "usage: surreallog <cmd> [args...]"
		return "", make([]string, 0), errors.New(msg)
	}

	return os.Args[1], os.Args[2:], nil
}

type options struct {
	endpoint string
	user     string
	pass     string
	ns       string
	db       string
	cd       time.Duration
	mbs      uint64
}

func getOptions() (*options, error) {
	endpoint, err := url.Parse(os.Getenv(envPrefix + "ENDPOINT"))
	if err != nil {
		return nil, err
	}

	user := os.Getenv(envPrefix + "USER")
	if user == "" {
		return nil, errors.New("env." + envPrefix + "USER not found")
	}

	pass := os.Getenv(envPrefix + "PASS")
	if pass == "" {
		return nil, errors.New("env." + envPrefix + "PASS not found")
	}

	ns := os.Getenv(envPrefix + "NAMESPACE")
	if ns == "" {
		return nil, errors.New("env." + envPrefix + "NAMESPACE not found")
	}

	name := os.Getenv(envPrefix + "NAME")
	if name == "" {
		name = "{{ hostname }}"
	}

	hostnameRe := regexp.MustCompile(`{{\s*hostname\s*}}`)
	if hostnameRe.MatchString(name) {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, err
		}

		name = hostnameRe.ReplaceAllString(name, hostname)
	}

	var cd time.Duration
	if env, found := os.LookupEnv(envPrefix + "CHUNK_DURATION"); found {
		cd, err = time.ParseDuration(env)
		if err != nil {
			return nil, err
		}
	} else {
		cd = 2 * time.Second
	}

	var mbs uint64
	if env, found := os.LookupEnv(envPrefix + "MAX_BUFFER_SIZE"); found {
		mbs, err = humanize.ParseBytes(env)
		if err != nil {
			return nil, err
		}
	} else {
		mbs = 1048576 // 2 MiB
	}

	opt := &options{
		endpoint: endpoint.String(),
		user:     user,
		pass:     pass,
		ns:       ns,
		db:       name,
		cd:       cd,
		mbs:      mbs,
	}

	return opt, nil
}

const (
	SETUP_QUERY_TEMPLATE = `
DEFINE NAMESPACE IF NOT EXISTS %s; -- 0
USE NS %s;                         -- 1

DEFINE DATABASE IF NOT EXISTS %s; -- 2
USE DB %s;                        -- 3

DEFINE TABLE IF NOT EXISTS counter SCHEMAFULL;        -- 4
DEFINE FIELD IF NOT EXISTS value ON counter TYPE int; -- 5

UPSERT ONLY counter:tb SET value += 1 RETURN VALUE value; -- 6

DEFINE TABLE IF NOT EXISTS catalog SCHEMAFULL;                           -- 7
DEFINE FIELD IF NOT EXISTS startedAt   ON catalog TYPE option<datetime>; -- 8
DEFINE FIELD IF NOT EXISTS completedAt ON catalog TYPE option<datetime>; -- 9
DEFINE FIELD IF NOT EXISTS exitCode    ON catalog TYPE option<int>;      -- 10
`

	DEFINE_TABLE_QUERY_TEMPLATE = `
CREATE catalog:%s RETURN NONE; -- 0

DEFINE TABLE %s SCHEMAFULL;                           -- 1
DEFINE FIELD kind ON %s TYPE -1 | 1 | 2;              -- 2
DEFINE FIELD time ON %s TYPE datetime;                -- 3
DEFINE FIELD text ON %s TYPE string;                  -- 4
DEFINE FIELD data ON %s TYPE option<string>;          -- 5
DEFINE FIELD opts ON %s FLEXIBLE TYPE option<object>; -- 6`

	START_QUERY_TEMPLATE = `
UPDATE catalog:%s SET startedAt = time::now() RETURN NONE; -- 0`

	COMPLETE_QUERY_TEMPLATE = `
UPDATE catalog:%s SET completedAt = time::now(), exitCode = $code RETURN NONE; -- 0`

	INSERT_LINES_QUERY_TEMPLATE = `
INSERT INTO %s $data RETURN NONE; -- 0`
)

type completeQueryVars struct {
	Code int `cbor:"code"`
}

type insertLinesQueryVars struct {
	Data []*cborLine `cbor:"data"`
}

type table struct {
	rid   string
	ident string
}

func initSurrealDB(db *sdb.SDB, opt *options) (*table, error) {
	if err := db.Signin(opt.user, opt.pass); err != nil {
		return nil, err
	}

	nsIdent := sdb.EscapeIdent(opt.ns)
	dbIdent := sdb.EscapeIdent(opt.db)
	q := fmt.Sprintf(SETUP_QUERY_TEMPLATE, nsIdent, nsIdent, dbIdent, dbIdent)
	r, err := db.Query(q, struct{}{})
	if err != nil {
		return nil, err
	}

	i, err := sdb.At[int](r, 6)
	if err != nil {
		return nil, err
	}

	err = db.Use(opt.ns, opt.db)
	if err != nil {
		return nil, err
	}

	ti := fmt.Sprintf(`#%d`, *i)
	tb := &table{
		rid:   sdb.EscapeRid(ti),
		ident: sdb.EscapeIdent(ti),
	}
	q = fmt.Sprintf(DEFINE_TABLE_QUERY_TEMPLATE, tb.rid, tb.ident, tb.ident, tb.ident, tb.ident, tb.ident, tb.ident)
	_, err = db.Query(q, struct{}{})
	if err != nil {
		return nil, err
	}

	return tb, nil
}

func getSurreal(opt *options) (*sdb.SDB, *table, error) {
	db := &sdb.SDB{}

	err := db.Connect(opt.endpoint)
	if err != nil {
		return nil, nil, err
	}

	tb, err := initSurrealDB(db, opt)
	if err != nil {
		db.Close()
		return nil, nil, err
	}

	return db, tb, nil
}

func getCmdEnv() []string {
	osEnv := os.Environ()
	var cmdEnv []string
	for _, env := range osEnv {
		if !strings.HasPrefix(env, envPrefix) {
			cmdEnv = append(cmdEnv, env)
		}
	}

	return cmdEnv
}

type line struct {
	kind int
	time *time.Time
	size int
	text string
	data string
	opts map[string]any
}

func newLine(fd1 bool, size int, text string) *line {
	k := 1
	if !fd1 {
		k = 2
	}
	t := time.Now()
	return &line{
		kind: k,
		time: &t,
		size: size,
		text: text,
	}
}

func newCommand(size int, c *ghc.GHC) (*line, error) {
	o := map[string]any{}
	if c.Opts != nil {
		var err error
		o, err = c.Opts.Map()
		if err != nil {
			return nil, err
		}
	}

	t := time.Now()
	return &line{
		kind: -1,
		time: &t,
		size: size,
		text: c.Name,
		data: string(c.Data),
		opts: o,
	}, nil
}

type cborLine struct {
	Kind int            `cbor:"kind"`
	Time *cbor.Tag      `cbor:"time"`
	Text string         `cbor:"text"`
	Data string         `cbor:"data,omitempty"`
	Opts map[string]any `cbor:"opts,omitempty"`
}

func toCborLine(l *line) *cborLine {
	return &cborLine{
		Kind: l.kind,
		Time: sdb.Datetime(l.time),
		Text: l.text,
		Data: l.data,
		Opts: l.opts,
	}
}

type sender struct {
	db      *sdb.SDB
	q       string
	buf     []*cborLine
	bufSize uint64
	mu      sync.Mutex
	timer   *time.Timer
	opt     *options
}

func newSender(db *sdb.SDB, tb *table, opt *options) *sender {
	return &sender{
		db:  db,
		q:   fmt.Sprintf(INSERT_LINES_QUERY_TEMPLATE, tb.ident),
		buf: []*cborLine{},
		opt: opt,
	}
}

func (s *sender) write(l *line) {
	if l == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.buf = append(s.buf, toCborLine(l))
	s.bufSize += uint64(l.size)

	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.opt.cd, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.flush()
	})

	if s.bufSize >= s.opt.mbs { // 1 MiB
		s.flush()
	}
}

func (s *sender) flush() {
	l := len(s.buf)
	if l == 0 {
		return
	}

	_, err := s.db.Query(s.q, &insertLinesQueryVars{s.buf})
	if err != nil {
		slog.Warn(err.Error())
	} else {
		slog.Debug("insert " + strconv.Itoa(l) + " line(s)")
	}

	s.buf = s.buf[:0]
	s.bufSize = 0

	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}

func splitFunc(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\n':
			if i > 0 && data[i-1] == '\r' {
				return i + 1, data[:i-1], nil // CRLF
			}

			return i + 1, data[:i], nil // LF

		case '\r':
			if i == len(data)-1 || data[i+1] != '\n' {
				return i + 1, data[:i], nil // CR
			}
		}
	}

	if atEOF {
		return len(data), data, nil
	}

	return 0, nil, nil
}

var masking = []byte("***")

func mask(s []byte, masks [][]byte) []byte {
	for _, m := range masks {
		s = bytes.ReplaceAll(s, m, masking)
	}

	return s
}

func streamReader(wg *sync.WaitGroup, r io.Reader, l chan<- *line, fd1 bool) {
	wg.Add(1)
	defer wg.Done()

	buf := make([]byte, 4096)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(buf, 65536)
	scanner.Split(splitFunc)
	masks := [][]byte{}
	enable := true
	endtoken := ""
	for scanner.Scan() {
		s := scanner.Bytes()
		if fd1 && enable {
			if c, _ := ghc.PraseGHC(s); c != nil {
				switch c.Name {
				case "debug":
					c.OmitOpts()
					c.Data = mask(c.Data, masks)
					cc, err := newCommand(len(s), c)
					if err != nil {
						break
					}
					l <- cc
					continue

				case "notice", "warning", "error":
					c.Data = mask(c.Data, masks)
					c.Opts.String("title")
					c.Opts.StringWithDefault("file", ".github")
					c.Opts.NaturalNum("col")
					c.Opts.NaturalNum("endColumn")
					c.Opts.NaturalNumWithDefault("line", 1)
					c.Opts.NaturalNumWithDefault("endLine", 1)
					cc, err := newCommand(len(s), c)
					if err != nil {
						break
					}
					l <- cc
					continue

				case "group":
					c.Data = mask(c.Data, masks)
					c.OmitOpts()
					cc, err := newCommand(len(s), c)
					if err != nil {
						break
					}
					l <- cc
					continue

				case "endgroup":
					c.NameOnly()
					cc, err := newCommand(len(s), c)
					if err != nil {
						break
					}
					l <- cc
					continue

				case "add-mask":
					if len(c.Data) > 0 && len(ghc.TrimLeftSpace(c.Data)) > 0 {
						masks = append(masks, c.Data)
						continue
					}

				case "stop-commands":
					if !enable {
						enable = false
						endtoken = string(c.Data)
						continue
					}

				default:
					if !enable && c.Name == endtoken {
						enable = true
						endtoken = ""
						continue
					}
				}
			}
		}

		s = mask(s, masks)
		l <- newLine(fd1, len(s), string(s))
	}
}

func runCmd(cmd *exec.Cmd, db *sdb.SDB, tb *table, opt *options) (int, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 1, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 1, err
	}

	q := fmt.Sprintf(START_QUERY_TEMPLATE, tb.rid)
	_, err = db.Query(q, struct{}{})
	if err != nil {
		return 1, err
	}

	lineChan := make(chan *line, 10)
	doneChan := make(chan error)

	go func() {
		var wg sync.WaitGroup

		go streamReader(&wg, stdout, lineChan, true)
		go streamReader(&wg, stderr, lineChan, false)

		slog.Debug("start")
		err := cmd.Run()

		wg.Wait()
		close(lineChan)

		doneChan <- err
		close(doneChan)
	}()

	s := newSender(db, tb, opt)
	for {
		select {
		case err := <-doneChan:
			for l := range lineChan {
				if l == nil {
					break
				}

				s.write(l)
			}

			if err != nil {
				l := newLine(false, 0, err.Error())
				s.write(l)
			}

			s.flush()

			return cmd.ProcessState.ExitCode(), err

		case l := <-lineChan:
			s.write(l)
		}
	}
}

func main() {
	slog.Debug("parsing command")
	name, args, err := getCommand()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	slog.Debug("parsing options")
	opt, err := getOptions()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	slog.Debug("preparing surrealdb")
	db, tb, err := getSurreal(opt)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	cmd := exec.Command(name, args...)
	cmd.Env = getCmdEnv()

	code, err := runCmd(cmd, db, tb, opt)
	if err != nil {
		slog.Error(err.Error())
	}

	q := fmt.Sprintf(COMPLETE_QUERY_TEMPLATE, tb.rid)
	_, err = db.Query(q, completeQueryVars{code})
	if err != nil {
		slog.Error(err.Error())
	}

	err = db.Close()
	if err != nil {
		slog.Error(err.Error())
	}

	slog.Info("completed with exit code " + strconv.Itoa(code))

	os.Exit(code)
}
