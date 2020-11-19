// ------------------------------------------------------------------------- //

package main

import (
  "context"
  "github.com/gammazero/nexus/v3/client"
  "github.com/gammazero/nexus/v3/wamp"
)

func TestFunction(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
  x, _ := wamp.AsInt64(inv.Arguments[0])
  y, _ := wamp.AsInt64(inv.Arguments[1])
  z := x + y
  return client.InvokeResult{Args: wamp.List{z}}
}

func sum(ctx context.Context, inv *wamp.Invocation) client.InvokeResult {
    var sum int64
    for _, arg := range inv.Arguments {
        n, _ := wamp.AsInt64(arg)
        sum += n
    }
    return client.InvokeResult{Args: wamp.List{sum}}
}


// ------------------------------------------------------------------------- //
