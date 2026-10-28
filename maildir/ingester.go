package maildir

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mxhook/mxhook/internal/metrics"
	"github.com/mxhook/mxhook/queue"
)

const (
	defaultPollInterval    = 2 * time.Second
	defaultMaxMessageBytes = 10 * 1024 * 1024
	defaultRecipientHeader = "Delivered-To"
	ownerMarkerName        = ".mxhook-owner"
	ownerMarkerContent     = "mxhook-maildir-v1\n"
	lockFileName           = ".mxhook.lock"
)

type Config struct {
	Path             string
	PollInterval     time.Duration
	MaxMessageBytes  int64
	RecipientHeader  string
	DefaultRecipient string
	IDNamespace      string
	AdoptExisting    bool
}

type Ingester struct {
	cfg         Config
	enqueue     queue.Enqueuer
	newDir      string
	curDir      string
	failedDir   string
	idNamespace string
	lockFile    lockHandle
	processMu   sync.Mutex
	closed      bool
	closeOnce   sync.Once
	closeErr    error
}

func New(cfg Config, enqueue queue.Enqueuer) (*Ingester, error) {
	if enqueue == nil {
		return nil, errors.New("maildir: enqueuer is required")
	}
	if cfg.Path == "" {
		return nil, errors.New("maildir: path is required")
	}
	absPath, err := filepath.Abs(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("maildir: resolve path: %w", err)
	}
	cfg.Path = absPath
	if cfg.PollInterval == 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.PollInterval < 0 {
		return nil, errors.New("maildir: poll interval must be positive")
	}
	if cfg.MaxMessageBytes == 0 {
		cfg.MaxMessageBytes = defaultMaxMessageBytes
	}
	if cfg.MaxMessageBytes < 0 {
		return nil, errors.New("maildir: max message bytes must be positive")
	}
	if cfg.RecipientHeader == "" {
		cfg.RecipientHeader = defaultRecipientHeader
	}
	if cfg.DefaultRecipient != "" {
		if _, err := parseSingleAddress(cfg.DefaultRecipient); err != nil {
			return nil, fmt.Errorf("maildir: invalid default recipient: %w", err)
		}
	}

	i := &Ingester{
		cfg:         cfg,
		enqueue:     enqueue,
		newDir:      filepath.Join(absPath, "new"),
		curDir:      filepath.Join(absPath, "cur"),
		failedDir:   filepath.Join(absPath, "mxhook-failed"),
		idNamespace: cfg.IDNamespace,
	}
	if i.idNamespace == "" {
		i.idNamespace = absPath
	}
	if err := i.initializeOwnership(); err != nil {
		return nil, err
	}
	if err := i.acquireLock(); err != nil {
		return nil, err
	}
	return i, nil
}

// Close releases the exclusive Maildir lock.
func (i *Ingester) Close() error {
	i.closeOnce.Do(func() {
		i.processMu.Lock()
		defer i.processMu.Unlock()
		i.closed = true
		if i.lockFile == nil {
			return
		}
		i.closeErr = i.lockFile.Close()
	})
	return i.closeErr
}

// Run polls the Maildir until ctx is canceled (returning ctx.Err()) or the
// ingester is closed (returning errIngesterClosed).
func (i *Ingester) Run(ctx context.Context) error {
	if err := i.pollOnce(ctx, "maildir: process backlog"); err != nil {
		return err
	}
	ticker := time.NewTicker(i.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := i.pollOnce(ctx, "maildir: process"); err != nil {
				return err
			}
		}
	}
}

// pollOnce runs one scan, logs scan errors that are not pure cancellation,
// and returns non-nil only when Run must stop (ingester closed).
func (i *Ingester) pollOnce(ctx context.Context, logMessage string) error {
	_, err := i.ProcessOnce(ctx)
	if err == nil {
		return nil
	}
	if errors.Is(err, errIngesterClosed) {
		return err
	}
	if err := withoutCanceled(err); err != nil {
		slog.Error(logMessage, "err", err)
	}
	return nil
}

// withoutCanceled strips context-cancellation errors, including members of a
// joined error, so shutdown does not suppress real scan errors that were
// joined with the cancellation sentinel.
func withoutCanceled(err error) error {
	if err == nil || !errors.Is(err, context.Canceled) {
		return err
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var kept []error
		for _, member := range joined.Unwrap() {
			if member = withoutCanceled(member); member != nil {
				kept = append(kept, member)
			}
		}
		return errors.Join(kept...)
	}
	return nil
}

var errIngesterClosed = errors.New("maildir: ingester is closed")

func (i *Ingester) ProcessOnce(ctx context.Context) (int, error) {
	i.processMu.Lock()
	defer i.processMu.Unlock()
	if i.closed {
		return 0, errIngesterClosed
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	claimErr := i.claimNew(ctx)
	processed, processErr := i.processClaimed(ctx)
	return processed, errors.Join(claimErr, processErr)
}

func (i *Ingester) initializeOwnership() error {
	for _, dir := range []string{i.cfg.Path, i.newDir, i.curDir, filepath.Join(i.cfg.Path, "tmp")} {
		if err := validatePrivateDirectory(dir); err != nil {
			return err
		}
	}

	marker := filepath.Join(i.cfg.Path, ownerMarkerName)
	content, err := readOwnershipMarker(marker)
	if err == nil {
		if string(content) != ownerMarkerContent {
			return errors.New("maildir: unsupported ownership marker")
		}
		return ensurePrivateDirectory(i.failedDir)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("maildir: read ownership marker: %w", err)
	}
	curEntries, err := os.ReadDir(i.curDir)
	if err != nil {
		return fmt.Errorf("maildir: read cur: %w", err)
	}
	if len(curEntries) != 0 && !i.cfg.AdoptExisting {
		return errors.New("maildir: cur is not empty; explicit adoption is required")
	}
	if err := writeOwnershipMarker(marker); err != nil {
		return err
	}
	return ensurePrivateDirectory(i.failedDir)
}

func writeOwnershipMarker(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: path is the operator-configured Maildir root joined with a constant marker name
	if err != nil {
		return fmt.Errorf("maildir: create ownership marker: %w", err)
	}
	if _, err := io.WriteString(f, ownerMarkerContent); err != nil {
		_ = f.Close() // best-effort cleanup on error path; original error returned
		return fmt.Errorf("maildir: write ownership marker: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close() // best-effort cleanup on error path; original error returned
		return fmt.Errorf("maildir: sync ownership marker: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("maildir: close ownership marker: %w", err)
	}
	return nil
}

func (i *Ingester) acquireLock() error {
	path := filepath.Join(i.cfg.Path, lockFileName)
	lockFile, err := openOwnershipLock(path)
	if err != nil {
		return err
	}
	i.lockFile = lockFile
	return nil
}

func (i *Ingester) claimNew(ctx context.Context) error {
	entries, err := os.ReadDir(i.newDir)
	if err != nil {
		return fmt.Errorf("maildir: scan new: %w", err)
	}
	var errs []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		source := filepath.Join(i.newDir, name)
		info, err := os.Lstat(source)
		if err != nil {
			errs = append(errs, fmt.Errorf("maildir: inspect new %q: %w", name, err))
			continue
		}
		if !info.Mode().IsRegular() {
			if err := i.quarantine(source, name); err != nil {
				errs = append(errs, fmt.Errorf("maildir: quarantine non-regular %q: %w", name, err))
			} else {
				errs = append(errs, fmt.Errorf("maildir: rejected non-regular file %q", name))
			}
			continue
		}
		claimedName := maildirUniqueName(name) + ":2,"
		target := filepath.Join(i.curDir, claimedName)
		if err := renameNoReplace(source, target); err != nil {
			errs = append(errs, fmt.Errorf("maildir: claim %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

func (i *Ingester) processClaimed(ctx context.Context) (int, error) {
	entries, err := os.ReadDir(i.curDir)
	if err != nil {
		return 0, fmt.Errorf("maildir: scan cur: %w", err)
	}
	processed := 0
	var errs []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(i.curDir, name)
		if err := i.processFile(ctx, path, name); err != nil {
			errs = append(errs, err)
			continue
		}
		processed++
	}
	return processed, errors.Join(errs...)
}

func (i *Ingester) processFile(ctx context.Context, path, name string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("maildir: inspect claimed %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		if err := i.quarantine(path, name); err != nil {
			return fmt.Errorf("maildir: quarantine claimed %q: %w", name, err)
		}
		return fmt.Errorf("maildir: rejected non-regular claimed file %q", name)
	}
	body, err := readLimited(path, info, i.cfg.MaxMessageBytes)
	if err != nil {
		if errors.Is(err, errMessageTooLarge) {
			if moveErr := i.quarantine(path, name); moveErr != nil {
				return errors.Join(err, moveErr)
			}
		}
		return fmt.Errorf("maildir: read %q: %w", name, err)
	}
	envelopeFrom, envelopeTo, err := i.envelopeMetadata(body)
	if err != nil {
		if moveErr := i.quarantine(path, name); moveErr != nil {
			return errors.Join(err, moveErr)
		}
		return fmt.Errorf("maildir: metadata %q: %w", name, err)
	}
	uniqueName := maildirUniqueName(name)
	msg := queue.NewMessage(
		i.messageID(uniqueName),
		"maildir",
		uniqueName,
		envelopeFrom,
		envelopeTo,
		info.ModTime(),
		body,
	)
	if err := i.enqueue.Enqueue(ctx, msg); err != nil {
		err = fmt.Errorf("maildir: enqueue %q: %w", name, err)
		// An ID collision means this filename was already enqueued with
		// different bytes; retrying can never succeed, so quarantine it.
		if errors.Is(err, queue.ErrIDCollision) {
			if moveErr := i.quarantine(path, name); moveErr != nil {
				return errors.Join(err, moveErr)
			}
		}
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("maildir: remove processed %q: %w", name, err)
	}
	// Counted after removal so a retained claim (enqueue succeeded, removal
	// failed) is not re-counted on every retry: the retried enqueue dedupes
	// to a no-op and the counter moves once per message. A crash between
	// enqueue and removal still re-counts once on restart (at-least-once).
	metrics.MessagesReceived.Inc()
	return nil
}

var errMessageTooLarge = errors.New("message exceeds configured size limit")

func readLimited(path string, expected os.FileInfo, limit int64) ([]byte, error) {
	f, err := openClaimedFile(path, expected)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only handle; close error not actionable
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("maildir: read message: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, errMessageTooLarge
	}
	return body, nil
}

func (i *Ingester) envelopeMetadata(body []byte) (string, []string, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("parse headers: %w", err)
	}
	recipientValue := msg.Header.Get(i.cfg.RecipientHeader)
	if strings.TrimSpace(recipientValue) == "" {
		recipientValue = i.cfg.DefaultRecipient
	}
	if strings.TrimSpace(recipientValue) == "" {
		return "", nil, fmt.Errorf("missing %s and default recipient", i.cfg.RecipientHeader)
	}
	recipient, err := parseSingleAddress(recipientValue)
	if err != nil {
		return "", nil, fmt.Errorf("invalid %s: %w", i.cfg.RecipientHeader, err)
	}
	sender, err := parseReturnPath(msg.Header.Get("Return-Path"))
	if err != nil {
		return "", nil, fmt.Errorf("invalid Return-Path: %w", err)
	}
	return sender, []string{recipient}, nil
}

func parseSingleAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("address is empty")
	}
	address, err := mail.ParseAddress(value)
	if err != nil {
		return "", fmt.Errorf("parse address: %w", err)
	}
	if at := strings.LastIndexByte(address.Address, '@'); at <= 0 || at == len(address.Address)-1 {
		return "", errors.New("address must contain a local part and domain")
	}
	return address.Address, nil
}

func parseReturnPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "<>" {
		return "", nil
	}
	return parseSingleAddress(value)
}

func (i *Ingester) messageID(uniqueName string) string {
	sum := sha256.Sum256([]byte(i.idNamespace + "\x00" + uniqueName))
	return fmt.Sprintf("maildir:%x", sum[:])
}

func maildirUniqueName(name string) string {
	if idx := strings.LastIndex(name, ":2,"); idx >= 0 {
		return name[:idx]
	}
	return name
}

func (i *Ingester) quarantine(source, name string) error {
	err := renameNoReplace(source, filepath.Join(i.failedDir, name))
	if err == nil || !errors.Is(err, os.ErrExist) {
		return err
	}
	// A message with this name was quarantined before; retry with unique suffixes.
	for range 5 {
		suffix := make([]byte, 8)
		if _, err := rand.Read(suffix); err != nil {
			return fmt.Errorf("maildir: quarantine suffix: %w", err)
		}
		target := filepath.Join(i.failedDir, fmt.Sprintf("%s.%d.%x", name, time.Now().UnixNano(), suffix))
		err = renameNoReplace(source, target)
		if err == nil || !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return fmt.Errorf("maildir: quarantine %q: %w", name, err)
}
