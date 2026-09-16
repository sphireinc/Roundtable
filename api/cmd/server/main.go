package main

import("context";"flag";"fmt";"os";"os/signal";"syscall";"roundtable/api/internal/httpapi")

func main(){ addr:=flag.String("addr","127.0.0.1:8080","HTTP listen address"); flag.Parse(); ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM); defer stop(); if err:=httpapi.NewServer(httpapi.Config{}).Serve(ctx,*addr);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)} }
