// ------------------------------------------------------------------------- //

package main

import (
	"fmt"
	"time"
)

func main() {

	start := time.Now()
	fmt.Println("...starting deviceagent at ",start)

	x := TestFunction(12,6)

	fmt.Println(x)

}


// ------------------------------------------------------------------------- //
