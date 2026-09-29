// Command cordis demonstrates dependency loss, restart and owned event cleanup.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

type counter interface{ Next() int }
type memoryCounter struct{ value int }

// Next increments this example's serially accessed counter.
func (c *memoryCounter) Next() int { c.value++; return c.value }

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer) (result error) {
	root := cordis.New()
	defer func() { result = errors.Join(result, root.Close(context.Background())) }()
	key := cordis.NewKey[counter]("counter")
	consumer, err := root.Plugin(cordis.Plugin{Name: "greeter", Inject: []string{key.Name()}, Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		service, err := cordis.Resolve(ctx, key)
		if err != nil {
			return nil, err
		}
		fmt.Fprintln(out, "greeter started")
		_, err = ctx.On("ready", func(e cordis.Event, _ cordis.Next) (any, error) {
			fmt.Fprintf(out, "%s #%d\n", e.Args[0], service.Next())
			return nil, nil
		}, cordis.EventOptions{})
		if err != nil {
			return nil, err
		}
		return func() error { fmt.Fprintln(out, "greeter stopped"); return nil }, nil
	}}, nil)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "before provider:", consumer.State())
	provider := cordis.Plugin{Name: "counter", Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
		_, err := cordis.Provide[counter](ctx, key, &memoryCounter{})
		return nil, err
	}}
	p, err := root.Plugin(provider, nil)
	if err != nil {
		return err
	}
	if err := root.Events().Emit("ready", "hello"); err != nil {
		return err
	}
	if err := p.Dispose(); err != nil {
		return err
	}
	fmt.Fprintln(out, "after provider removal:", consumer.State())
	if err := root.Events().Emit("ready", "not delivered"); err != nil {
		return err
	}
	if _, err := root.Plugin(provider, nil); err != nil {
		return err
	}
	return root.Events().Emit("ready", "hello again")
}
