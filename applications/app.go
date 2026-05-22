package applications

import (
	"context"
	"sync"
)

type App interface {
	Run(ctx context.Context, wg *sync.WaitGroup)
}
