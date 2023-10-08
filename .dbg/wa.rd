# 编译调试
{
    alias gob='CGO_ENABLED=0 go build -v -gcflags "all=-N -l"'
    # alias dlv='gob && dlv exec ./wa --init=.dbg/wa.dlv -- run waroot/examples/copy.wa'
    alias dlv='gob && dlv exec ./wa --init=.dbg/wa.dlv -- run /home/qstesiro/github.com/qstesiro/demo/wa-demo'
}
