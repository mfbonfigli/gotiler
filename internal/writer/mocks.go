package writer

import (
	"context"
	"sync"

	"github.com/mfbonfigli/gotiler/v3/tiler/tree"
)

type MockProducer struct {
	Wc  chan *WorkUnit
	Ec  chan error
	Err error
	Wu  *WorkUnit
}

func (m *MockProducer) Produce(workchan chan *WorkUnit, errchan chan error, wg *sync.WaitGroup, node tree.Node, ctx context.Context) {
	m.Wc = workchan
	m.Ec = errchan
	defer close(workchan)
	if m.Err != nil {
		errchan <- m.Err
	} else if m.Wu != nil {
		workchan <- m.Wu
	}
	wg.Done()
}

type MockConsumer struct {
	Wc  chan *WorkUnit
	Ec  chan error
	Err error
}

func (m *MockConsumer) Consume(workchan chan *WorkUnit, errchan chan error, waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	m.Wc = workchan
	m.Ec = errchan
	if m.Err != nil {
		errchan <- m.Err
	}
}

type MockWriter struct {
	Err         error
	Tr          tree.Tree
	FolderName  string
	Ctx         context.Context
	WriteCalled bool
}

func (m *MockWriter) Write(t tree.Tree, folderName string, ctx context.Context, reporter tree.ProgressReporter) error {
	m.WriteCalled = true
	m.Tr = t
	m.FolderName = folderName
	m.Ctx = ctx
	return m.Err
}
