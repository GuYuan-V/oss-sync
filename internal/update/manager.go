package update

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Manager 提供持久化的已校验候选与操作状态机
type Manager struct {
	mu   sync.Mutex
	root string
	path string
	st   persistedState
}

// NewManager 在 root 下创建或恢复 Manager
func NewManager(root string) (*Manager, error) {
	path := stateFilePath(root)
	// 同一 root 的并发创建者经 root 级锁串行化
	rm := rootMutex(root)
	rm.Lock()
	defer rm.Unlock()
	release, err := acquireFileLock(root)
	if err != nil {
		return nil, err
	}
	defer release()
	st, err := loadState(path)
	if err != nil {
		return &Manager{root: root, path: path, st: newEmptyState()}, err
	}
	m := &Manager{root: root, path: path, st: st}
	m.pruneExpiredLocked()
	_ = m.persistLocked()
	return m, nil
}

func (m *Manager) pruneExpiredLocked() {
	now := time.Now().UnixNano()
	for id, cc := range m.st.Checked {
		if cc.ExpiresAt > 0 && now > cc.ExpiresAt {
			delete(m.st.Checked, id)
		}
	}
}

func (m *Manager) persistLocked() error {
	return atomicWriteJSONFn(m.path, m.st)
}

func (m *Manager) reloadLocked() error {
	fresh, err := loadState(m.path)
	if err != nil {
		return err
	}
	m.st = fresh
	return nil
}

// IssueChecked 颁发带过期时间的已校验候选，调用前须通过 Candidate.Validate
func (m *Manager) IssueChecked(c Candidate, ttl time.Duration) (CheckedCandidate, error) {
	if err := c.Validate(); err != nil {
		return CheckedCandidate{}, err
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	now := time.Now()
	cc := CheckedCandidate{ID: uuid.NewString(), Candidate: c, CreatedAt: now.UnixNano(), ExpiresAt: now.Add(ttl).UnixNano()}
	rm := rootMutex(m.root)
	rm.Lock()
	defer rm.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := acquireFileLock(m.root)
	if err != nil {
		return CheckedCandidate{}, err
	}
	defer release()
	if err := m.reloadLocked(); err != nil {
		// 状态损坏时直接返回错误
		return CheckedCandidate{}, err
	}
	m.st.Checked[cc.ID] = cc
	if err := m.persistLocked(); err != nil {
		delete(m.st.Checked, cc.ID)
		return CheckedCandidate{}, err
	}
	return cloneChecked(cc), nil
}

// ValidateChecked 按 checkID 取回候选，不存在或过期时返回类型化错误
func (m *Manager) ValidateChecked(id string) (*Candidate, error) {
	rm := rootMutex(m.root)
	rm.Lock()
	defer rm.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := acquireFileLock(m.root)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := m.reloadLocked(); err != nil {
		return nil, err
	}
	cc, ok := m.st.Checked[id]
	if !ok {
		return nil, newUpdateError(CodeCheckNotFound, "checked candidate not found: "+id, ErrCheckNotFound)
	}
	if time.Now().UnixNano() > cc.ExpiresAt {
		delete(m.st.Checked, id)
		_ = m.persistLocked()
		return nil, newUpdateError(CodeCheckExpired, "checked candidate expired: "+id, ErrCheckExpired)
	}
	return cloneCandidate(&cc.Candidate), nil
}

// GetChecked 按 ID 返回已校验候选的不可变快照
func (m *Manager) GetChecked(id string) (CheckedCandidate, error) {
	rm := rootMutex(m.root)
	rm.Lock()
	defer rm.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := acquireFileLock(m.root)
	if err != nil {
		return CheckedCandidate{}, err
	}
	defer release()
	if err := m.reloadLocked(); err != nil {
		return CheckedCandidate{}, err
	}
	cc, ok := m.st.Checked[id]
	if !ok {
		return CheckedCandidate{}, newUpdateError(CodeCheckNotFound, "checked candidate not found", ErrCheckNotFound)
	}
	if time.Now().UnixNano() > cc.ExpiresAt {
		return CheckedCandidate{}, newUpdateError(CodeCheckExpired, "checked candidate expired", ErrCheckExpired)
	}
	return cloneChecked(cc), nil
}

// StartOperation 按已校验候选原子地声明一次更新操作，同一时刻仅允许一个活跃操作
func (m *Manager) StartOperation(checkID string, targetVersion string) (*Operation, error) {
	rm := rootMutex(m.root)
	rm.Lock()
	defer rm.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := acquireFileLock(m.root)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := m.reloadLocked(); err != nil {
		return nil, err
	}
	if m.st.ActiveID != "" {
		if op, ok := m.st.Ops[m.st.ActiveID]; ok && !op.IsTerminal() {
			return nil, newUpdateError(CodeAlreadyInProgress, "operation already in progress: "+m.st.ActiveID, ErrAlreadyInProgress)
		}
	}
	cc, ok := m.st.Checked[checkID]
	if !ok {
		return nil, newUpdateError(CodeCheckNotFound, "checked candidate not found", ErrCheckNotFound)
	}
	if time.Now().UnixNano() > cc.ExpiresAt {
		delete(m.st.Checked, checkID)
		_ = m.persistLocked()
		return nil, newUpdateError(CodeCheckExpired, "checked candidate expired", ErrCheckExpired)
	}
	if targetVersion != "" && targetVersion != cc.Candidate.Version {
		return nil, newUpdateError(CodeCheckMismatch, "target version mismatch: want "+cc.Candidate.Version+" got "+targetVersion, ErrCheckMismatch)
	}
	now := time.Now().UTC()
	op := Operation{ID: uuid.NewString(), State: StateInProgress, Candidate: cloneCandidate(&cc.Candidate), StartedAt: now, UpdatedAt: now}
	m.st.Ops[op.ID] = op
	m.st.ActiveID = op.ID
	m.st.History = append(m.st.History, op.ID)
	m.st.retainHistory()
	if err := m.persistLocked(); err != nil {
		delete(m.st.Ops, op.ID)
		m.st.ActiveID = ""
		return nil, err
	}
	cp := cloneOperation(op)
	return &cp, nil
}

// Transition 按允许的状态图推进操作状态，非法迁移返回类型化错误
func (m *Manager) Transition(opID string, next OperationState, errMsg string) (*Operation, error) {
	rm := rootMutex(m.root)
	rm.Lock()
	defer rm.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	release, err := acquireFileLock(m.root)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := m.reloadLocked(); err != nil {
		return nil, err
	}
	op, ok := m.st.Ops[opID]
	if !ok {
		return nil, newUpdateError(CodeCheckNotFound, "operation not found", ErrCheckNotFound)
	}
	if !next.IsValid() {
		return nil, newUpdateError(CodeInvalidTransition, "invalid target state: "+string(next), ErrInvalidTransition)
	}
	if op.IsTerminal() {
		return nil, newUpdateError(CodeInvalidTransition, "cannot transition from terminal state "+string(op.State), ErrInvalidTransition)
	}
	if !isAllowedTransition(op.State, next) {
		return nil, newUpdateError(CodeInvalidTransition, "transition "+string(op.State)+" -> "+string(next)+" not allowed", ErrInvalidTransition)
	}
	op.State = next
	op.UpdatedAt = time.Now().UTC()
	if errMsg != "" {
		op.Error = errMsg
	}
	m.st.Ops[opID] = op
	if next.IsTerminal() && m.st.ActiveID == opID {
		m.st.ActiveID = ""
	}
	m.st.retainHistory()
	if err := m.persistLocked(); err != nil {
		return nil, err
	}
	cp := cloneOperation(op)
	return &cp, nil
}

// GetOperation 按 ID 返回操作的不可变快照
func (m *Manager) GetOperation(id string) (*Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 跨实例可见性要求重读磁盘状态，尽力而为，不强制持有写锁文件
	if fresh, err := loadState(m.path); err == nil {
		m.st = fresh
	}
	op, ok := m.st.Ops[id]
	if !ok {
		return nil, newUpdateError(CodeCheckNotFound, "operation not found", ErrCheckNotFound)
	}
	cp := cloneOperation(op)
	return &cp, nil
}

func (m *Manager) QueryOperation(id string) (*Operation, error) {
	return m.GetOperation(id)
}

// ActiveOperation 返回当前活跃操作的快照，无活跃操作时返回空
func (m *Manager) ActiveOperation() *Operation {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fresh, err := loadState(m.path); err == nil {
		m.st = fresh
	}
	if m.st.ActiveID == "" {
		return nil
	}
	op, ok := m.st.Ops[m.st.ActiveID]
	if !ok || op.IsTerminal() {
		return nil
	}
	cp := cloneOperation(op)
	return &cp
}

// ListHistory 按持久化顺序返回操作历史快照
func (m *Manager) ListHistory() []Operation {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fresh, err := loadState(m.path); err == nil {
		m.st = fresh
	}
	out := make([]Operation, 0, len(m.st.History))
	for _, id := range m.st.History {
		if op, ok := m.st.Ops[id]; ok {
			out = append(out, cloneOperation(op))
		}
	}
	return out
}

// CurrentStatus 返回对外暴露的精简状态，不含下载地址与可执行路径
func (m *Manager) CurrentStatus() ManagerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fresh, err := loadState(m.path); err == nil {
		m.st = fresh
	}
	var active *PublicOperation
	if m.st.ActiveID != "" {
		if op, ok := m.st.Ops[m.st.ActiveID]; ok && !op.IsTerminal() {
			p := toPublic(op)
			active = &p
		}
	}
	hist := make([]PublicOperation, 0, len(m.st.History))
	for _, id := range m.st.History {
		if op, ok := m.st.Ops[id]; ok {
			hist = append(hist, toPublic(op))
		}
	}
	return ManagerStatus{Active: active, History: hist}
}
