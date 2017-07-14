package boltdb

import (
	"encoding/binary"
	"sync"

	"github.com/boltdb/bolt"
	"github.com/troian/surgemq/message"
	"github.com/troian/surgemq/persistence/types"
)

const (
	bucketRetained      = "retained"
	bucketSessions      = "sessions"
	bucketMessages      = "messages"
	bucketSubscriptions = "subscriptions"
)

type dbStatus struct {
	db   *bolt.DB
	done chan struct{}
}

type impl struct {
	db dbStatus

	// transactions that are in progress right now
	wgTx sync.WaitGroup
	lock sync.Mutex

	r retained
	s sessions
}

type sessions struct {
	db *dbStatus

	// transactions that are in progress right now
	wgTx *sync.WaitGroup
	lock *sync.Mutex
}

type session struct {
	db *dbStatus

	id string

	s subscriptions
	m messages
}

type subscriptions struct {
	db *dbStatus

	id string
}

type messages struct {
	db *dbStatus

	id string
}

type retained struct {
	db *dbStatus

	// transactions that are in progress right now
	wgTx *sync.WaitGroup
	lock *sync.Mutex

	//tx *boltDB.Tx
}

// NewBoltDB allocate new persistence provider of boltDB type
func NewBoltDB(config *types.BoltDBConfig) (p types.Provider, err error) {
	pl := &impl{
		db: dbStatus{
			done: make(chan struct{}),
		},
	}

	if pl.db.db, err = bolt.Open(config.File, 0600, nil); err != nil {
		return nil, err
	}

	pl.r = retained{
		db:   &pl.db,
		wgTx: &pl.wgTx,
		lock: &pl.lock,
	}

	pl.s = sessions{
		db:   &pl.db,
		wgTx: &pl.wgTx,
		lock: &pl.lock,
	}

	p = pl

	return p, nil
}

// Sessions
func (p *impl) Sessions() (types.Sessions, error) {
	select {
	case <-p.db.done:
		return nil, types.ErrNotOpen
	default:
	}

	return &p.s, nil
}

// Retained
func (p *impl) Retained() (types.Retained, error) {
	select {
	case <-p.db.done:
		return nil, types.ErrNotOpen
	default:
	}

	return &p.r, nil
}

// Shutdown provider
func (p *impl) Shutdown() error {
	p.lock.Lock()
	defer p.lock.Unlock()

	select {
	case <-p.db.done:
		return types.ErrNotOpen
	default:
	}

	close(p.db.done)

	p.wgTx.Wait()

	err := p.db.db.Close()
	p.db.db = nil

	return err
}

// New
func (s *sessions) New(id string) (types.Session, error) {
	select {
	case <-s.db.done:
		return nil, types.ErrNotOpen
	default:
	}

	ses := newSession(s.db, id)

	err := s.db.db.Update(func(tx *bolt.Tx) error {
		sesBucket, err := tx.CreateBucketIfNotExists([]byte(bucketSessions))
		if err != nil {
			return err
		}

		_, err = sesBucket.CreateBucket([]byte(id))

		return err
	})

	if err != nil {
		if err == bolt.ErrBucketExists {
			return nil, types.ErrAlreadyExists
		}
	}

	return &ses, nil
}

// Get
func (s *sessions) Get(id string) (types.Session, error) {
	select {
	case <-s.db.done:
		return nil, types.ErrNotOpen
	default:
	}

	err := s.db.db.View(func(tx *bolt.Tx) error {
		sesBucket := tx.Bucket([]byte(bucketSessions))
		if sesBucket == nil {
			return types.ErrNotFound
		}

		if buck := sesBucket.Bucket([]byte(id)); buck == nil {
			return types.ErrNotFound
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	ses := newSession(s.db, id)

	return &ses, nil
}

func (s *sessions) GetAll() ([]types.Session, error) {
	select {
	case <-s.db.done:
		return nil, types.ErrNotOpen
	default:
	}

	res := []types.Session{}

	err := s.db.db.View(func(tx *bolt.Tx) error {
		sesBucket := tx.Bucket([]byte(bucketSessions))
		if sesBucket == nil {
			return types.ErrNotFound
		}

		c := sesBucket.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			// make sure it is bucket
			if buck := sesBucket.Bucket(k); buck != nil {
				ses := newSession(s.db, string(k))

				res = append(res, &ses)
			}
		}

		return nil
	})

	return res, err
}

// Delete
func (s *sessions) Delete(id string) error {
	select {
	case <-s.db.done:
		return types.ErrNotOpen
	default:
	}

	err := s.db.db.Update(func(tx *bolt.Tx) error {
		// get sessions bucket
		sesBucket := tx.Bucket([]byte(bucketSessions))
		if sesBucket == nil {
			return types.ErrNotFound
		}

		return sesBucket.DeleteBucket([]byte(id))
	})

	if err != nil {
		return types.ErrNotFound
	}

	return nil
}

func newSession(db *dbStatus, id string) session {
	ses := session{
		db: db,
		id: id,
	}

	ses.m = messages{
		db: db,
		id: id,
	}

	ses.s = subscriptions{
		db: db,
		id: id,
	}

	return ses
}

// Subscriptions
func (s *session) Subscriptions() (types.Subscriptions, error) {
	select {
	case <-s.db.done:
		return nil, types.ErrNotOpen
	default:
	}

	return &s.s, nil
}

// Messages
func (s *session) Messages() (types.Messages, error) {
	select {
	case <-s.db.done:
		return nil, types.ErrNotOpen
	default:
	}

	return &s.m, nil
}

func (s *session) ID() (string, error) {
	select {
	case <-s.db.done:
		return "", types.ErrNotOpen
	default:
	}

	return s.id, nil
}

func (s *subscriptions) Add(subs message.TopicsQoS) error {
	select {
	case <-s.db.done:
		return types.ErrNotOpen
	default:
	}

	return s.db.db.Update(func(tx *bolt.Tx) error {
		// get sessions bucket
		sesBucket := tx.Bucket([]byte(bucketSessions))
		if sesBucket == nil {
			return types.ErrNotFound
		}
		// get bucket for given session
		sBucket := sesBucket.Bucket([]byte(s.id))
		if sBucket == nil {
			return types.ErrNotFound
		}

		bucket, err := sBucket.CreateBucketIfNotExists([]byte(bucketSubscriptions))
		if err != nil {
			return err
		}
		for t, q := range subs {
			id, _ := bucket.NextSequence() // nolint: gas

			var pb *bolt.Bucket
			if pb, err = bucket.CreateBucket(itob64(id)); err != nil {
				return err
			}

			if err := pb.Put([]byte("topic"), []byte(t)); err != nil {
				return err
			}

			if err := pb.Put([]byte("qos"), []byte{byte(q)}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *subscriptions) Get() (message.TopicsQoS, error) {
	select {
	case <-s.db.done:
		return nil, types.ErrNotOpen
	default:
	}

	res := make(message.TopicsQoS)
	err := s.db.db.View(func(tx *bolt.Tx) error {
		// get sessions bucket
		sesBucket := tx.Bucket([]byte(bucketSessions))
		if sesBucket == nil {
			return types.ErrNotFound
		}

		// get bucket for given session
		sBucket := sesBucket.Bucket([]byte(s.id))
		if sBucket == nil {
			return types.ErrNotFound
		}

		bucket := sBucket.Bucket([]byte(bucketSubscriptions))
		if bucket == nil {
			return types.ErrNotFound
		}

		return bucket.ForEach(func(k, v []byte) error {
			c := bucket.Cursor()
			for k, _ := c.First(); k != nil; k, _ = c.Next() {
				var t string
				var q message.QosType
				subBuck := bucket.Bucket(k)
				err := subBuck.ForEach(func(k, v []byte) error {
					name := string(k)
					switch name {
					case "topic":
						t = string(v)
					case "qos":
						q = message.QosType(v[0])
					}
					return nil
				})

				if err != nil {
					return err
				}

				res[t] = q
			}

			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	return res, nil
}

func (s *subscriptions) Delete() error {
	select {
	case <-s.db.done:
		return types.ErrNotOpen
	default:
	}

	return s.db.db.Update(func(tx *bolt.Tx) error {
		// get sessions bucket
		sesBucket := tx.Bucket([]byte(bucketSessions))
		if sesBucket == nil {
			return types.ErrNotFound
		}

		// get bucket for given session
		sBucket := sesBucket.Bucket([]byte(s.id))
		if sBucket == nil {
			return types.ErrNotFound
		}

		return sBucket.DeleteBucket([]byte(bucketSubscriptions))
	})
}

// Store
func (m *messages) Store(dir string, msg []message.Provider) error {
	select {
	case <-m.db.done:
		return types.ErrNotOpen
	default:
	}

	return m.db.db.Update(func(tx *bolt.Tx) error {
		// get sessions bucket
		sesBucket := tx.Bucket([]byte(bucketSessions))
		if sesBucket == nil {
			return types.ErrNotFound
		}

		// get bucket for given session
		sBucket := sesBucket.Bucket([]byte(m.id))
		if sBucket == nil {
			return types.ErrNotFound
		}

		bucket, err := sBucket.CreateBucketIfNotExists([]byte(bucketMessages))
		if err != nil {
			return err
		}

		dirBuck, err := bucket.CreateBucketIfNotExists([]byte(dir))
		if err != nil {
			return err
		}

		for _, m := range msg {
			id, _ := dirBuck.NextSequence() // nolint: gas
			var pb *bolt.Bucket
			if pb, err = dirBuck.CreateBucket(itob64(id)); err != nil {
				return err
			}

			if err = putMsg(pb, m); err != nil {
				return err
			}
		}

		return nil
	})
}

// Load
func (m *messages) Load() (*types.SessionMessages, error) {
	select {
	case <-m.db.done:
		return nil, types.ErrNotOpen
	default:
	}

	msg := types.SessionMessages{}
	err := m.db.db.View(func(tx *bolt.Tx) error {
		// get sessions bucket
		sesBucket := tx.Bucket([]byte(bucketSessions))
		if sesBucket == nil {
			return types.ErrNotFound
		}

		// get bucket for given session
		sBucket := sesBucket.Bucket([]byte(m.id))
		if sBucket == nil {
			return types.ErrNotFound
		}

		msgBuck := sBucket.Bucket([]byte(bucketMessages))
		if msgBuck == nil {
			return types.ErrNotFound
		}

		if dirBuck := msgBuck.Bucket([]byte("in")); dirBuck != nil {
			msg.In.Messages, _ = getMsgs(dirBuck) // nolint: gas
		}

		if dirBuck := msgBuck.Bucket([]byte("out")); dirBuck != nil {
			msg.Out.Messages, _ = getMsgs(dirBuck) // nolint: gas
		}

		return nil
	})

	return &msg, err
}

// Delete
func (m *messages) Delete() error {
	select {
	case <-m.db.done:
		return types.ErrNotOpen
	default:
	}

	return m.db.db.Update(func(tx *bolt.Tx) error {
		// get sessions bucket
		sesBucket := tx.Bucket([]byte(bucketSessions))
		if sesBucket == nil {
			return types.ErrNotFound
		}

		// get bucket for given session
		sBucket := sesBucket.Bucket([]byte(m.id))
		if sBucket == nil {
			return types.ErrNotFound
		}

		return sBucket.DeleteBucket([]byte(bucketMessages))
	})
}

// Load
func (r *retained) Load() ([]message.Provider, error) {
	select {
	case <-r.db.done:
		return nil, types.ErrNotOpen
	default:
	}

	msg := []message.Provider{}
	err := r.db.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(bucketRetained))
		if bucket == nil {
			return types.ErrNotFound
		}
		var err error
		msg, err = getMsgs(bucket)
		return err
	})

	return msg, err
}

// Store
func (r *retained) Store(msg []message.Provider) error {
	select {
	case <-r.db.done:
		return types.ErrNotOpen
	default:
	}

	return r.db.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(bucketRetained))
		if err != nil {
			return err
		}

		for _, m := range msg {
			id, _ := bucket.NextSequence() // nolint: gas
			var pb *bolt.Bucket
			if pb, err = bucket.CreateBucket(itob64(id)); err != nil {
				return err
			}
			err = putMsg(pb, m)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// Delete
func (r *retained) Delete() error {
	select {
	case <-r.db.done:
		return types.ErrNotOpen
	default:
	}

	err := r.db.db.Update(func(tx *bolt.Tx) error {
		return tx.DeleteBucket([]byte(bucketRetained))
	})

	if err != nil {
		if err == bolt.ErrBucketNotFound {
			err = types.ErrNotFound
		}
	}

	return err
}

func getMsgs(b *bolt.Bucket) ([]message.Provider, error) {
	entries := []message.Provider{}

	c := b.Cursor()
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		packBuk := b.Bucket(k)
		// firstly get id to decide what message type this is
		tmp := packBuk.Get([]byte("type"))

		mT, err := message.Type(tmp[0]).NewMessage()
		if err != nil {
			return nil, err
		}
		err = packBuk.ForEach(func(name []byte, val []byte) error {
			var e error
			switch m := mT.(type) {
			case *message.PublishMessage:
				switch string(name) {
				case "id":
					m.SetPacketID(binary.BigEndian.Uint16(val))
				case "topic":
					e = m.SetTopic(string(val))
				case "payload":
					buf := make([]byte, len(val))
					copy(buf, val)
					m.SetPayload(buf)
				case "qos":
					e = m.SetQoS(message.QosType(val[0]))
				}
			}

			return e
		})
		if err != nil {
			return nil, err
		}

		entries = append(entries, mT)
	}

	return entries, nil
}

func putMsg(b *bolt.Bucket, msg message.Provider) error {
	if err := b.Put([]byte("type"), []byte{byte(msg.Type())}); err != nil {
		return err
	}

	if msg.PacketID() != 0 {
		if err := b.Put([]byte("id"), itob16(msg.PacketID())); err != nil {
			return err
		}
	}

	switch m := msg.(type) {
	case *message.PublishMessage:
		if err := b.Put([]byte("qos"), []byte{byte(m.QoS())}); err != nil {
			return err
		}

		if err := b.Put([]byte("topic"), []byte(m.Topic())); err != nil {
			return err
		}

		if len(m.Payload()) > 0 {
			if err := b.Put([]byte("payload"), m.Payload()); err != nil {
				return err
			}
		}
	case *message.PubRelMessage:
		// have nothing to do here
	}

	return nil
}

// itob returns an 8-byte big endian representation of v.
func itob64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func itob16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}
