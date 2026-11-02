package lib

import "fmt"

type Service struct{}

func Start()  { Finish() }
func Finish() { fmt.Println("done") }
