// 版权 @2021 凹语言 作者。保留所有权利。

package compiler_wat

import (
	"strconv"
	"strings"

	"wa-lang.org/wa/internal/constant"
	"wa-lang.org/wa/internal/loader"
	"wa-lang.org/wa/internal/token"
	"wa-lang.org/wa/internal/types"

	"wa-lang.org/wa/internal/backends/compiler_wat/wir"
	"wa-lang.org/wa/internal/backends/compiler_wat/wir/wat"
	"wa-lang.org/wa/internal/logger"
	"wa-lang.org/wa/internal/ssa"
)

type valueWrap struct {
	value          wir.Value
	force_register bool
}

type functionGenerator struct {
	prog   *loader.Program
	module *wir.Module
	tLib   *typeLib

	locals_map map[ssa.Value]valueWrap

	registers    []wir.Value
	cur_local_id int

	var_block_selector wir.Value
	var_current_block  wir.Value
	var_rets           []wir.Value

	is_init bool
}

func newFunctionGenerator(prog *loader.Program, module *wir.Module, tLib *typeLib) *functionGenerator {
	return &functionGenerator{prog: prog, module: module, tLib: tLib, locals_map: make(map[ssa.Value]valueWrap)}
}

func (g *functionGenerator) getValue(i ssa.Value) valueWrap {
	if i == nil {
		return valueWrap{}
	}

	if v, ok := g.locals_map[i]; ok {
		return v
	}

	if v := g.module.FindGlobalByValue(i); v != nil {
		return valueWrap{value: v}
	}

	switch v := i.(type) {
	case *ssa.Const:
		vt := v.Type()
		switch t := vt.(type) {
		case *types.Basic:
			switch t.Kind() {

			case types.Bool, types.UntypedBool:
				if constant.BoolVal(v.Value) {
					return valueWrap{value: wir.NewConst("1", g.module.BOOL)}
				} else {
					return valueWrap{value: wir.NewConst("0", g.module.BOOL)}
				}

			case types.Uint8:
				val, _ := constant.Uint64Val(v.Value)
				return valueWrap{value: wir.NewConst(strconv.Itoa(int(val)), g.module.U8)}

			//case types.Int8:
			//	val, _ := constant.Int64Val(v.Value)
			//	return valueWrap{value: wir.NewConst(strconv.Itoa(int(val)), g.module.I8)}

			case types.Uint16:
				val, _ := constant.Uint64Val(v.Value)
				return valueWrap{value: wir.NewConst(strconv.Itoa(int(val)), g.module.U16)}

			//case types.Int16:
			//	val, _ := constant.Int64Val(v.Value)
			//	return valueWrap{value: wir.NewConst(strconv.Itoa(int(val)), g.module.I16)}

			case types.Uint32, types.Uintptr, types.Uint:
				val, _ := constant.Uint64Val(v.Value)
				return valueWrap{value: wir.NewConst(strconv.Itoa(int(val)), g.module.U32)}

			case types.Int32, types.Int:
				val, _ := constant.Int64Val(v.Value)
				if t.Name() == "rune" {
					return valueWrap{value: wir.NewConst(strconv.Itoa(int(val)), g.module.RUNE)}
				} else {
					return valueWrap{value: wir.NewConst(strconv.Itoa(int(val)), g.module.I32)}
				}

			case types.Int64:
				val, _ := constant.Int64Val(v.Value)
				return valueWrap{value: wir.NewConst(strconv.Itoa(int(val)), g.module.I64)}

			case types.Uint64:
				val, _ := constant.Uint64Val(v.Value)
				return valueWrap{value: wir.NewConst(strconv.Itoa(int(val)), g.module.U64)}

			case types.Float32:
				val, _ := constant.Float64Val(v.Value)
				return valueWrap{value: wir.NewConst(strconv.FormatFloat(val, 'f', -1, 32), g.module.F32)}

			case types.Float64:
				val, _ := constant.Float64Val(v.Value)
				return valueWrap{value: wir.NewConst(strconv.FormatFloat(val, 'f', -1, 64), g.module.F64)}

			case types.String, types.UntypedString:
				val := constant.StringVal(v.Value)
				return valueWrap{value: wir.NewConst(val, g.module.STRING)}

			default:
				logger.Fatalf("Todo:%T %v", t, t.Kind())
			}

		case *types.Pointer:
			if v.Value == nil {
				p_type := g.tLib.compile(t)
				return valueWrap{value: wir.NewConst("0", p_type)}
			} else {
				logger.Fatalf("Todo:%T", t)
			}

		case *types.Slice:
			if v.Value == nil {
				return valueWrap{value: wir.NewConst("0", g.tLib.compile(t))}
			}
			logger.Fatalf("Todo:%T", t)

		case *types.Interface:
			if v.Value == nil {
				return valueWrap{value: wir.NewConst("0", g.tLib.compile(t))}
			}
			logger.Fatalf("Todo:%T", t)

		case *types.Named:
			if v.Value == nil {
				return valueWrap{value: wir.NewConst("0", g.tLib.compile(t))}
			}
			if _, ok := t.Underlying().(*types.Basic); ok {
				return valueWrap{value: wir.NewConst(v.Value.String(), g.tLib.compile(t))}
			}
			logger.Fatalf("Todo:%T", t)

		case *types.Signature:
			if v.Value == nil {
				return valueWrap{value: wir.NewConst("0", g.tLib.compile(t))}
			}
			logger.Fatalf("Todo:%T", t)

		default:
			logger.Fatalf("Todo:%T", t)
		}

	case ssa.Instruction:
		nv := valueWrap{value: g.addRegister(g.tLib.compile(i.Type()))}
		g.locals_map[i] = nv
		return nv

	case *ssa.Function:
		fn_name, _ := wir.GetFnMangleName(v)

		if v.Parent() != nil {
			if g.module.FindFunc(fn_name) == nil {
				g.module.AddFunc(newFunctionGenerator(g.prog, g.module, g.tLib).genFunction(v))
			}
		}

		return valueWrap{value: g.module.GenConstFnValue(fn_name, g.tLib.GenFnSig(v.Signature))}
	}

	logger.Fatal("Value not found:", i)
	return valueWrap{}
}

func (g *functionGenerator) genFunction(f *ssa.Function) *wir.Function {
	g.is_init = (f.Synthetic == "package initializer")
	var wir_fn wir.Function
	// 函数名称与导出名
	{
		internal, external := wir.GetFnMangleName(f)
		if len(f.LinkName()) > 0 {
			wir_fn.InternalName = f.LinkName()
		} else {
			wir_fn.InternalName = internal
		}
		if len(f.ExportName()) > 0 {
			wir_fn.ExternalName = f.ExportName()
		} else {
			wir_fn.ExternalName = external
		}
	}
	// 函数返回
	rets := f.Signature.Results()
	switch rets.Len() {
	case 0:
		break

	case 1:
		wir_fn.Results = append(wir_fn.Results, g.tLib.compile(rets.At(0).Type()))

	default:
		typ := g.tLib.compile(rets).(*wir.Tuple)
		wir_fn.Results = append(wir_fn.Results, typ.Fields...)
	}

	for _, i := range f.FreeVars { // ???
		fv := valueWrap{value: wir.NewLocal(wir.GenSymbolName(i.Name()), g.tLib.compile(i.Type()))}
		wir_fn.Params = append(wir_fn.Params, fv.value)
		g.locals_map[i] = fv
	}
	for _, i := range f.Params {
		pv := valueWrap{value: wir.NewLocal(wir.GenSymbolName(i.Name()), g.tLib.compile(i.Type()))}
		wir_fn.Params = append(wir_fn.Params, pv.value)
		g.locals_map[i] = pv
	}

	g.var_block_selector = wir.NewLocal("$block_selector", g.module.I32)
	g.registers = append(g.registers, g.var_block_selector)
	g.var_current_block = wir.NewLocal("$current_block", g.module.I32)
	g.registers = append(g.registers, g.var_current_block)
	for i, rt := range wir_fn.Results {
		rname := "$ret_" + strconv.Itoa(i)
		r := wir.NewLocal(rname, rt)
		g.var_rets = append(g.var_rets, r)
		g.registers = append(g.registers, r)
	}

	var block_temp wat.Inst
	//BlockSel:
	{
		inst := wat.NewInstBlock("$BlockSel")
		inst.Insts = append(inst.Insts, g.var_block_selector.EmitPush()...)
		t := make([]int, len(f.Blocks)+1)
		for i := range f.Blocks {
			t[i] = i
		}
		t[len(f.Blocks)] = 0
		inst.Insts = append(inst.Insts, wat.NewInstBrTable(t))
		block_temp = inst
	}

	for i, b := range f.Blocks {
		block := wat.NewInstBlock("$Block_" + strconv.Itoa(i))
		block.Insts = append(block.Insts, block_temp)
		block.Insts = append(block.Insts, g.genBlock(b)...)
		block_temp = block
	}

	//BlockDisp
	{
		inst := wat.NewInstLoop("$BlockDisp")
		inst.Insts = append(inst.Insts, block_temp)
		block_temp = inst
	}

	//BlockFnBody
	{
		inst := wat.NewInstBlock("$BlockFnBody")
		inst.Insts = append(inst.Insts, block_temp)
		block_temp = inst
	}

	//for _, i := range g.registers {
	//	wir_fn.Insts = append(wir_fn.Insts, i.EmitInit()...)
	//}

	wir_fn.Insts = append(wir_fn.Insts, block_temp)

	for _, r := range g.var_rets {
		wir_fn.Insts = append(wir_fn.Insts, r.EmitPush()...)
	}

	for _, i := range g.registers {
		wir_fn.Insts = append(wir_fn.Insts, i.EmitRelease()...)
	}

	wir_fn.Locals = g.registers

	return &wir_fn
}

func (g *functionGenerator) genBlock(block *ssa.BasicBlock) []wat.Inst {
	if len(block.Instrs) == 0 {
		logger.Fatalf("Block:%s is empty", block)
	}

	cur_block_assigned := false
	var b []wat.Inst
	for _, inst := range block.Instrs {
		if _, ok := inst.(*ssa.Phi); !ok {
			if !cur_block_assigned {
				b = append(b, g.module.EmitAssginValue(g.var_current_block, wir.NewConst(strconv.Itoa(block.Index), g.module.I32))...)
				b = append(b, wat.NewBlank())
				cur_block_assigned = true
			}
		}

		b = append(b, g.genInstruction(inst)...)
	}
	return b

}

func (g *functionGenerator) genInstruction(inst ssa.Instruction) (insts []wat.Inst) {
	insts = append(insts, wat.NewComment(inst.String()))

	switch inst := inst.(type) {

	case *ssa.If:
		insts = append(insts, g.genIf(inst)...)

	case *ssa.Store:
		insts = append(insts, g.genStore(inst)...)

	case *ssa.Jump:
		insts = append(insts, g.genJump(inst)...)

	case *ssa.Return:
		insts = append(insts, g.genReturn(inst)...)

	case ssa.Value:
		s, t := g.genValue(inst)
		if t != nil && !t.Equal(g.module.VOID) {
			if v, ok := g.locals_map[inst]; ok {
				if !v.value.Type().Equal(t) {
					panic("Type not match")
				}
				s = append(s, v.value.EmitPop()...)
			} else {
				nv := g.addRegister(t)
				g.locals_map[inst] = valueWrap{value: nv}
				s = append(s, nv.EmitPop()...)
			}
		}
		insts = append(insts, s...)

	case *ssa.Panic:
		insts = append(insts, g.genPanic(inst)...)

	default:
		logger.Fatalf("Todo: %[1]v: %[1]T", inst)
	}
	insts = append(insts, wat.NewBlank())
	return
}

func (g *functionGenerator) genValue(v ssa.Value) ([]wat.Inst, wir.ValueType) {
	//if _, ok := g.locals_map[v]; ok {
	//	logger.Printf("Instruction already exist：%s\n", v)
	//}

	switch v := v.(type) {
	case *ssa.Range:
		return g.genRange(v)

	case *ssa.Next:
		return g.genNext(v)
	}

	//Todo: 下面的做法过于粗暴
	g.tLib.compile(v.Type())

	switch v := v.(type) {
	case *ssa.UnOp:
		return g.genUnOp(v)

	case *ssa.BinOp:
		return g.genBinOp(v)

	case *ssa.Call:
		return g.genCall(v)

	case *ssa.Phi:
		return g.genPhi(v)

	case *ssa.Alloc:
		return g.genAlloc(v)

	case *ssa.Extract:
		return g.genExtract(v)

	case *ssa.Field:
		return g.genFiled(v)

	case *ssa.FieldAddr:
		return g.genFieldAddr(v)

	case *ssa.IndexAddr:
		return g.genIndexAddr(v)

	case *ssa.Index:
		return g.genIndex(v)

	case *ssa.Slice:
		return g.genSlice(v)

	case *ssa.MakeSlice:
		return g.genMakeSlice(v)

	case *ssa.Lookup:
		return g.genLookup(v)

	case *ssa.Convert:
		return g.genConvert(v)

	case *ssa.ChangeType:
		return g.genChangeType(v)

	case *ssa.MakeClosure:
		return g.genMakeClosre(v)

	case *ssa.MakeInterface:
		return g.genMakeInterface(v)

	case *ssa.ChangeInterface:
		return g.genChangeInterface(v)

	case *ssa.TypeAssert:
		return g.genTypeAssert(v)
	}

	logger.Fatalf("Todo: %v, type: %T", v, v)
	return nil, nil
}

func (g *functionGenerator) genUnOp(inst *ssa.UnOp) (insts []wat.Inst, ret_type wir.ValueType) {
	switch inst.Op {
	case token.MUL: //*x
		return g.genLoad(inst.X)

	case token.SUB:
		x := g.getValue(inst.X)
		return g.module.EmitUnOp(x.value, wat.OpCodeSub)

	case token.XOR:
		x := g.getValue(inst.X)
		return g.module.EmitUnOp(x.value, wat.OpCodeXor)

	case token.NOT:
		x := g.getValue(inst.X)
		return g.module.EmitUnOp(x.value, wat.OpCodeNot)

	default:
		logger.Fatalf("Todo: %[1]v: %[1]T", inst)
	}

	return
}

func (g *functionGenerator) genBinOp(inst *ssa.BinOp) ([]wat.Inst, wir.ValueType) {
	x := g.getValue(inst.X)
	y := g.getValue(inst.Y)

	switch inst.Op {
	case token.ADD:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeAdd)

	case token.SUB:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeSub)

	case token.MUL:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeMul)

	case token.QUO:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeQuo)

	case token.REM:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeRem)

	case token.EQL:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeEql)

	case token.NEQ:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeNe)

	case token.LSS:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeLt)

	case token.GTR:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeGt)

	case token.LEQ:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeLe)

	case token.GEQ:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeGe)

	case token.AND:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeAnd)

	case token.OR:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeOr)

	case token.XOR:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeXor)

	case token.SHL:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeShl)

	case token.SHR:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeShr)

	case token.AND_NOT:
		return g.module.EmitBinOp(x.value, y.value, wat.OpCodeAndNot)

	default:
		logger.Fatalf("Todo: %v, type: %T, token:%v", inst, x.value, inst.Op)
		return nil, nil
	}
}

func (g *functionGenerator) genCall(inst *ssa.Call) (insts []wat.Inst, ret_type wir.ValueType) {
	if inst.Call.IsInvoke() {
		ret_type = g.tLib.compile(inst.Call.Signature().Results())

		t := g.tLib.find(inst.Call.Value.Type())
		for id := 0; id < t.NumMethods(); id++ {
			m := t.Method(id)
			if m.Name == inst.Call.Method.Name() {
				iface := g.getValue(inst.Call.Value)

				var params []wir.Value
				for _, v := range inst.Call.Args {
					params = append(params, g.getValue(v).value)
				}

				insts = append(insts, g.module.EmitInvoke(iface.value, params, id, m.FullFnName)...)

				break
			}
		}

		return
		//logger.Fatal("Todo: genCall(), Invoke")
	}

	switch inst.Call.Value.(type) {
	case *ssa.Function:
		ret_type = g.tLib.compile(inst.Call.Signature().Results())
		for _, v := range inst.Call.Args {
			insts = append(insts, g.getValue(v).value.EmitPushNoRetain()...)
		}
		callee := inst.Call.StaticCallee()
		if callee.Parent() != nil {
			g.module.AddFunc(newFunctionGenerator(g.prog, g.module, g.tLib).genFunction(callee))
		}

		if len(callee.LinkName()) > 0 {
			insts = append(insts, wat.NewInstCall(callee.LinkName()))
		} else {
			fn_internal_name, _ := wir.GetFnMangleName(callee)
			insts = append(insts, wat.NewInstCall(fn_internal_name))
		}

	case *ssa.Builtin:
		return g.genBuiltin(inst.Common())

	case *ssa.MakeClosure:
		ret_type = g.tLib.compile(inst.Type())
		var params []wir.Value
		for _, v := range inst.Call.Args {
			params = append(params, g.getValue(v).value)
		}
		closure := g.getValue(inst.Call.Value)
		insts = wir.EmitCallClosure(closure.value, params)

	default:
		ret_type = g.tLib.compile(inst.Type())
		var params []wir.Value
		for _, v := range inst.Call.Args {
			params = append(params, g.getValue(v).value)
		}
		closure := g.getValue(inst.Call.Value)
		insts = wir.EmitCallClosure(closure.value, params)

	}

	return
}

func (g *functionGenerator) genBuiltin(call *ssa.CallCommon) (insts []wat.Inst, ret_type wir.ValueType) {
	switch call.Value.Name() {
	case "assert":
		for i, arg := range call.Args {
			av := g.getValue(arg).value
			avt := av.Type()

			// assert(ok: bool, ...)
			if i == 0 {
				if !avt.Equal(g.module.BOOL) {
					panic("call.Args[0] is not bool")
				}
				insts = append(insts, av.EmitPushNoRetain()...)
				continue
			}

			// assert(ok: bool, messag: string)
			if i == 1 {
				if !avt.Equal(g.module.STRING) {
					panic("call.Args[1] is not string")
				}
				insts = append(insts, g.module.EmitStringValue(av)...)
				continue
			}

			panic("unreachable")
		}

		// 位置信息
		{
			callPos := g.prog.Fset.Position(call.Pos())
			s := wir.NewConst(callPos.String(), g.module.STRING)
			insts = append(insts, g.module.EmitStringValue(s)...)
		}

		switch len(call.Args) {
		case 1:
			insts = append(insts, wat.NewInstCall("$runtime.assert"))
		case 2:
			insts = append(insts, wat.NewInstCall("$runtime.assertWithMessage"))
		default:
			panic("len(call.Args) == 1 or 2")
		}

	case "print", "println":
		for i, arg := range call.Args {
			av := g.getValue(arg).value
			avt := av.Type()

			if i > 0 {
				insts = append(insts, wat.NewInstConst(wat.I32{}, "32"))
				insts = append(insts, wat.NewInstCall("$runtime.waPrintRune"))
			}

			if avt.Equal(g.module.BOOL) {
				insts = append(insts, av.EmitPushNoRetain()...)
				insts = append(insts, wat.NewInstCall("$runtime.waPrintBool"))
			} else if avt.Equal(g.module.I32) {
				insts = append(insts, av.EmitPushNoRetain()...)
				insts = append(insts, wat.NewInstCall("$runtime.waPrintI32"))
			} else if avt.Equal(g.module.U8) || avt.Equal(g.module.U16) || avt.Equal(g.module.U32) {
				insts = append(insts, av.EmitPushNoRetain()...)
				insts = append(insts, wat.NewInstCall("$runtime.waPrintU32"))
			} else if avt.Equal(g.module.I64) {
				insts = append(insts, av.EmitPushNoRetain()...)
				insts = append(insts, wat.NewInstCall("$runtime.waPrintI64"))
			} else if avt.Equal(g.module.U64) {
				insts = append(insts, av.EmitPushNoRetain()...)
				insts = append(insts, wat.NewInstCall("$runtime.waPrintU64"))
			} else if avt.Equal(g.module.F32) {
				insts = append(insts, av.EmitPushNoRetain()...)
				insts = append(insts, wat.NewInstCall("$runtime.waPrintF32"))
			} else if avt.Equal(g.module.F64) {
				insts = append(insts, av.EmitPushNoRetain()...)
				insts = append(insts, wat.NewInstCall("$runtime.waPrintF64"))
			} else if avt.Equal(g.module.RUNE) {
				insts = append(insts, av.EmitPushNoRetain()...)
				insts = append(insts, wat.NewInstCall("$runtime.waPrintRune"))
			} else if avt.Equal(g.module.STRING) {
				insts = append(insts, g.module.EmitPrintString(av)...)
			} else if _, ok := avt.(*wir.Interface); ok {
				insts = append(insts, g.module.EmitPrintInterface(av)...)
			} else {
				logger.Fatalf("Todo: print(%T)", avt)
			}
		}

		if call.Value.Name() == "println" {
			insts = append(insts, wir.NewConst(strconv.Itoa('\n'), g.module.I32).EmitPushNoRetain()...)
			insts = append(insts, wat.NewInstCall("$runtime.waPrintRune"))
		}
		ret_type = g.module.VOID

	case "append":
		if len(call.Args) != 2 {
			panic("len(call.Args) != 2")
		}
		insts, ret_type = g.module.EmitGenAppend(g.getValue(call.Args[0]).value, g.getValue(call.Args[1]).value)

	case "len", "长":
		if len(call.Args) != 1 {
			panic("len(call.Args) != 1")
		}
		insts = g.module.EmitGenLen(g.getValue(call.Args[0]).value)
		ret_type = g.module.I32

	case "cap":
		if len(call.Args) != 1 {
			panic("len(cap.Args) != 1")
		}
		insts = g.module.EmitGenCap(g.getValue(call.Args[0]).value)
		ret_type = g.module.I32

	case "copy":
		if len(call.Args) != 2 {
			logger.Fatal("len(copy.Args) != 2")
		}
		insts = g.module.EmitGenCopy(g.getValue(call.Args[0]).value, g.getValue(call.Args[1]).value)
		ret_type = g.module.I32

	case "ssa:wrapnilchk":
		insts = g.getValue(call.Args[0]).value.EmitPushNoRetain()
		ret_type = g.getValue(call.Args[0]).value.Type()

	default:
		logger.Fatal("Todo:", call.Value)
	}
	return
}

func (g *functionGenerator) genPanic(panic_ *ssa.Panic) (insts []wat.Inst) {
	av := g.getValue(panic_.X).value
	avt := av.Type()

	if !avt.Equal(g.module.STRING) {
		panic("panic message is not string")
	}
	insts = append(insts, g.module.EmitStringValue(av)...)

	// 位置信息
	{
		callPos := g.prog.Fset.Position(panic_.Pos())
		s := wir.NewConst(callPos.String(), g.module.STRING)
		insts = append(insts, g.module.EmitStringValue(s)...)
	}

	insts = append(insts, wat.NewInstCall("$runtime.panic_"))
	return
}

func (g *functionGenerator) genPhiIter(preds []int, values []wir.Value) []wat.Inst {
	var insts []wat.Inst

	cond, _ := g.module.EmitBinOp(g.var_current_block, wir.NewConst(strconv.Itoa(preds[0]), g.module.I32), wat.OpCodeEql)
	insts = append(insts, cond...)

	trueInsts := values[0].EmitPush()
	var falseInsts []wat.Inst
	if len(preds) == 2 {
		falseInsts = values[1].EmitPush()
	} else {
		falseInsts = g.genPhiIter(preds[1:], values[1:])
	}
	insts = append(insts, wat.NewInstIf(trueInsts, falseInsts, values[0].Type().Raw()))

	return insts
}
func (g *functionGenerator) genPhi(inst *ssa.Phi) ([]wat.Inst, wir.ValueType) {
	var preds []int
	var values []wir.Value
	for i, v := range inst.Edges {
		preds = append(preds, inst.Block().Preds[i].Index)
		values = append(values, g.getValue(v).value)
	}
	return g.genPhiIter(preds, values), g.tLib.compile(inst.Type())
}

func (g *functionGenerator) genReturn(inst *ssa.Return) []wat.Inst {
	var insts []wat.Inst

	if len(inst.Results) != len(g.var_rets) {
		panic("len(inst.Results) != len(g.var_rets)")
	}

	for i := range inst.Results {
		insts = append(insts, g.module.EmitAssginValue(g.var_rets[i], g.getValue(inst.Results[i]).value)...)
	}

	insts = append(insts, wat.NewInstBr("$BlockFnBody"))
	return insts
}

func (g *functionGenerator) genLoad(Addr ssa.Value) (insts []wat.Inst, ret_type wir.ValueType) {
	addr := g.getValue(Addr)

	if addr.force_register {
		insts = append(insts, addr.value.EmitPush()...)
		ret_type = addr.value.Type()
	} else {
		insts, ret_type = g.module.EmitLoad(addr.value)
	}

	return
}

func (g *functionGenerator) genStore(inst *ssa.Store) []wat.Inst {
	addr := g.getValue(inst.Addr)
	val := g.getValue(inst.Val)

	if addr.force_register {
		return g.module.EmitAssginValue(addr.value, val.value)
	} else {
		return g.module.EmitStore(addr.value, val.value, g.is_init)
	}
}

func (g *functionGenerator) genIf(inst *ssa.If) []wat.Inst {
	cond := g.getValue(inst.Cond)
	if !cond.value.Type().Equal(g.module.BOOL) {
		logger.Fatal("cond.type() != bool")
	}

	insts := cond.value.EmitPush()
	instsTrue := g.genJumpID(inst.Block().Index, inst.Block().Succs[0].Index)
	instsFalse := g.genJumpID(inst.Block().Index, inst.Block().Succs[1].Index)
	insts = append(insts, wat.NewInstIf(instsTrue, instsFalse, nil))

	return insts
}

func (g *functionGenerator) genJump(inst *ssa.Jump) []wat.Inst {
	return g.genJumpID(inst.Block().Index, inst.Block().Succs[0].Index)
}

func (g *functionGenerator) genJumpID(cur, dest int) []wat.Inst {
	var insts []wat.Inst

	if cur >= dest {
		insts = g.module.EmitAssginValue(g.var_block_selector, wir.NewConst(strconv.Itoa(dest), g.module.I32))
		insts = append(insts, wat.NewInstBr("$BlockDisp"))
	} else {
		insts = append(insts, wat.NewInstBr("$Block_"+strconv.Itoa(dest-1)))
	}

	return insts
}

func (g *functionGenerator) genAlloc(inst *ssa.Alloc) (insts []wat.Inst, ret_type wir.ValueType) {
	typ := g.tLib.compile(inst.Type().(*types.Pointer).Elem())
	if inst.Parent().ForceRegister() {
		nv := g.addRegister(typ)
		g.locals_map[inst] = valueWrap{value: nv, force_register: true}
		insts = append(insts, nv.EmitRelease()...)
		insts = append(insts, nv.EmitInit()...)
		ret_type = nil
	} else {
		if inst.Heap {
			insts, ret_type = g.module.EmitHeapAlloc(typ)
		} else {
			insts, ret_type = g.module.EmitStackAlloc(typ)
		}
	}

	return
}

func (g *functionGenerator) genExtract(inst *ssa.Extract) ([]wat.Inst, wir.ValueType) {
	v := g.getValue(inst.Tuple)
	return g.module.EmitGenExtract(v.value, inst.Index)
}

func (g *functionGenerator) genFiled(inst *ssa.Field) ([]wat.Inst, wir.ValueType) {
	x := g.getValue(inst.X)
	return g.module.EmitGenField(x.value, inst.Field)
}

func (g *functionGenerator) genFieldAddr(inst *ssa.FieldAddr) (insts []wat.Inst, ret_type wir.ValueType) {
	x := g.getValue(inst.X)
	if x.force_register {
		nv := wir.ExtractFieldByID(x.value, inst.Field)
		g.locals_map[inst] = valueWrap{value: nv, force_register: true}
		return nil, nil
	} else {
		var ret_val wir.Value
		insts, ret_type, ret_val = g.module.EmitGenFieldAddr(x.value, inst.Field)
		if ret_val != nil {
			g.locals_map[inst] = valueWrap{value: ret_val}
			ret_type = nil
		}
		return
	}
}

func (g *functionGenerator) genIndexAddr(inst *ssa.IndexAddr) (insts []wat.Inst, ret_type wir.ValueType) {
	if inst.Parent().ForceRegister() {
		logger.Fatal("ssa.IndexAddr is not available in ForceRegister-mode")
		return nil, nil
	}

	x := g.getValue(inst.X)
	id := g.getValue(inst.Index)

	var ret_val wir.Value
	insts, ret_type, ret_val = g.module.EmitGenIndexAddr(x.value, id.value)
	if ret_val != nil {
		g.locals_map[inst] = valueWrap{value: ret_val}
		ret_type = nil
	}
	return
}

func (g *functionGenerator) genIndex(inst *ssa.Index) (insts []wat.Inst, ret_type wir.ValueType) {
	x := g.getValue(inst.X)
	id := g.getValue(inst.Index)

	return g.module.EmitGenIndex(x.value, id.value)
}

func (g *functionGenerator) genSlice(inst *ssa.Slice) ([]wat.Inst, wir.ValueType) {
	if inst.Parent().ForceRegister() {
		logger.Fatal("ssa.Slice is not available in ForceRegister-mode")
		return nil, nil
	}

	x := g.getValue(inst.X)
	var low, high, max wir.Value
	if inst.Low != nil {
		low = g.getValue(inst.Low).value
	}
	if inst.High != nil {
		high = g.getValue(inst.High).value
	}
	if inst.Max != nil {
		max = g.getValue(inst.Max).value
	}

	return g.module.EmitGenSlice(x.value, low, high, max)
}

func (g *functionGenerator) genMakeSlice(inst *ssa.MakeSlice) (insts []wat.Inst, ret_type wir.ValueType) {
	if inst.Parent().ForceRegister() {
		logger.Fatal("ssa.MakeSlice is not available in ForceRegister-mode")
		return nil, nil
	}

	src_type := inst.Type()
	ret_type = g.tLib.compile(src_type)
	Len := g.getValue(inst.Len)
	Cap := g.getValue(inst.Cap)

	insts = g.module.EmitGenMakeSlice(ret_type, Len.value, Cap.value)
	return
}

func (g *functionGenerator) genLookup(inst *ssa.Lookup) ([]wat.Inst, wir.ValueType) {
	x := g.getValue(inst.X)
	index := g.getValue(inst.Index)

	return g.module.EmitGenLookup(x.value, index.value, inst.CommaOk)
}

func (g *functionGenerator) genConvert(inst *ssa.Convert) (insts []wat.Inst, ret_type wir.ValueType) {
	x := g.getValue(inst.X)
	ret_type = g.tLib.compile(inst.Type())
	insts = g.module.EmitGenConvert(x.value, ret_type)
	return
}

func (g *functionGenerator) genChangeType(inst *ssa.ChangeType) (insts []wat.Inst, ret_type wir.ValueType) {
	ret_type = g.tLib.compile(inst.Type())
	x := g.getValue(inst.X)
	insts = append(insts, x.value.EmitPush()...)
	return
}

func (g *functionGenerator) genMakeClosre(inst *ssa.MakeClosure) (insts []wat.Inst, ret_type wir.ValueType) {
	f := inst.Fn.(*ssa.Function)

	if f.Parent() != nil {
		return g.genMakeClosre_Anonymous(inst)
	}

	if len(f.FreeVars) == 1 && strings.HasSuffix(f.Name(), "$bound") {
		return g.genMakeClosre_Bound(inst)
	}

	panic("todo")
}

func (g *functionGenerator) genMakeClosre_Anonymous(inst *ssa.MakeClosure) (insts []wat.Inst, ret_type wir.ValueType) {
	f := inst.Fn.(*ssa.Function)

	g.module.AddFunc(newFunctionGenerator(g.prog, g.module, g.tLib).genFunction(f))

	ret_type = g.module.GenValueType_Closure(g.tLib.GenFnSig(f.Signature))
	if !ret_type.Equal(g.tLib.compile(inst.Type())) {
		panic("ret_type != inst.Type()")
	}

	var st_free_data *wir.Struct
	{
		fn_internal_name, _ := wir.GetFnMangleName(f)
		st_name := fn_internal_name + ".$warpdata"

		var found bool
		st_free_data, found = g.module.GenValueType_Struct(st_name)
		if found {
			logger.Fatalf("Type: %s already registered.", st_name)
		}

		for _, freevar := range f.FreeVars {
			vtype := g.tLib.compile(freevar.Type())
			field := g.module.NewStructField(freevar.Name(), vtype)
			st_free_data.AppendField(field)
		}
		st_free_data.Finish()
	}

	var warp_fn_index int
	{
		var warp_fn wir.Function
		fn_name, _ := wir.GetFnMangleName(f)
		warp_fn.InternalName = fn_name + ".$warpfn"
		for _, i := range f.Params {
			pa := valueWrap{value: wir.NewLocal(i.Name(), g.tLib.compile(i.Type()))}
			warp_fn.Params = append(warp_fn.Params, pa.value)
		}
		warp_fn.Results = g.tLib.GenFnSig(f.Signature).Results

		dx := g.module.FindGlobalByName("$wa.runtime.closure_data")
		data_ptr := wir.ExtractFieldByName(dx, "d")

		warp_fn.Insts = append(warp_fn.Insts, st_free_data.EmitLoadFromAddr(data_ptr, 0)...)
		warp_fn.Insts = append(warp_fn.Insts, dx.EmitRelease()...)
		warp_fn.Insts = append(warp_fn.Insts, dx.EmitInit()...)

		for _, i := range warp_fn.Params {
			warp_fn.Insts = append(warp_fn.Insts, i.EmitPushNoRetain()...)
		}

		warp_fn.Insts = append(warp_fn.Insts, wat.NewInstCall(fn_name))

		g.module.AddFunc(&warp_fn)
		warp_fn_index = g.module.AddTableElem(warp_fn.InternalName)
	}

	closure := g.addRegister(g.module.GenValueType_Closure(g.tLib.GenFnSig(f.Signature)))

	free_data := g.addRegister(st_free_data)
	{
		for i, freevar := range f.FreeVars {
			sv := g.getValue(inst.Bindings[i])
			insts = append(insts, sv.value.EmitPush()...)
			dv := wir.ExtractFieldByName(free_data, freevar.Name())
			insts = append(insts, dv.EmitPop()...)
		}
	}
	insts = append(insts, wir.NewConst(strconv.Itoa(warp_fn_index), g.module.U32).EmitPush()...)
	insts = append(insts, wir.ExtractFieldByName(closure, "fn_index").EmitPop()...)
	{
		i, _ := g.module.EmitHeapAlloc(st_free_data)
		insts = append(insts, i...)
		insts = append(insts, wir.ExtractFieldByName(closure, "d").EmitPop()...)
	}
	insts = append(insts, g.module.EmitStore(wir.ExtractFieldByName(closure, "d"), free_data, false)...)
	insts = append(insts, free_data.EmitRelease()...)
	insts = append(insts, free_data.EmitInit()...)

	insts = append(insts, closure.EmitPush()...)
	return
}

func (g *functionGenerator) genMakeClosre_Bound(inst *ssa.MakeClosure) (insts []wat.Inst, ret_type wir.ValueType) {
	f := inst.Fn.(*ssa.Function)

	ret_type = g.module.GenValueType_Closure(g.tLib.GenFnSig(f.Signature))
	if !ret_type.Equal(g.tLib.compile(inst.Type())) {
		panic("ret_type != inst.Type()")
	}

	recv_type := g.tLib.compile(f.FreeVars[0].Type())

	var warp_fn_index int
	{
		var warp_fn wir.Function
		fn_name, _ := wir.GetFnMangleName(f.Object())
		warp_fn.InternalName = fn_name + ".$bound"
		for _, i := range f.Params {
			pa := valueWrap{value: wir.NewLocal(i.Name(), g.tLib.compile(i.Type()))}
			warp_fn.Params = append(warp_fn.Params, pa.value)
		}
		warp_fn.Results = g.tLib.GenFnSig(f.Signature).Results

		dx := g.module.FindGlobalByName("$wa.runtime.closure_data")
		data_ptr := wir.ExtractFieldByName(dx, "d")

		warp_fn.Insts = append(warp_fn.Insts, recv_type.EmitLoadFromAddr(data_ptr, 0)...)
		warp_fn.Insts = append(warp_fn.Insts, dx.EmitRelease()...)
		warp_fn.Insts = append(warp_fn.Insts, dx.EmitInit()...)

		for _, i := range warp_fn.Params {
			warp_fn.Insts = append(warp_fn.Insts, i.EmitPushNoRetain()...)
		}

		warp_fn.Insts = append(warp_fn.Insts, wat.NewInstCall(fn_name))

		g.module.AddFunc(&warp_fn)
		warp_fn_index = g.module.AddTableElem(warp_fn.InternalName)
	}

	closure := g.addRegister(g.module.GenValueType_Closure(g.tLib.GenFnSig(f.Signature)))

	insts = append(insts, wir.NewConst(strconv.Itoa(warp_fn_index), g.module.U32).EmitPush()...)
	insts = append(insts, wir.ExtractFieldByName(closure, "fn_index").EmitPop()...)
	{
		i, _ := g.module.EmitHeapAlloc(recv_type)
		insts = append(insts, i...)
		insts = append(insts, wir.ExtractFieldByName(closure, "d").EmitPop()...)
	}

	recv := g.getValue(inst.Bindings[0])
	insts = append(insts, g.module.EmitStore(wir.ExtractFieldByName(closure, "d"), recv.value, false)...)

	insts = append(insts, closure.EmitPush()...)
	return
}

func (g *functionGenerator) genMakeInterface(inst *ssa.MakeInterface) (insts []wat.Inst, ret_type wir.ValueType) {
	x := g.getValue(inst.X)
	ret_type = g.tLib.compile(inst.Type())
	insts = g.module.EmitGenMakeInterface(x.value, ret_type)
	return
}

func (g *functionGenerator) genChangeInterface(inst *ssa.ChangeInterface) (insts []wat.Inst, ret_type wir.ValueType) {
	x := g.getValue(inst.X)
	ret_type = g.tLib.compile(inst.Type())
	insts = g.module.EmitGenChangeInterface(x.value, ret_type)
	return
}

func (g *functionGenerator) genTypeAssert(inst *ssa.TypeAssert) (insts []wat.Inst, ret_type wir.ValueType) {
	x := g.getValue(inst.X)
	destType := g.tLib.compile(inst.AssertedType)
	if inst.CommaOk {
		ret_type = g.module.GenValueType_Tuple([]wir.ValueType{destType, g.module.BOOL})
	} else {
		ret_type = destType
	}
	insts = g.module.EmitGenTypeAssert(x.value, destType, inst.CommaOk)
	return
}

func (g *functionGenerator) genRange(inst *ssa.Range) (insts []wat.Inst, ret_type wir.ValueType) {
	x := g.getValue(inst.X)
	return g.module.EmitGenRange(x.value)
}

func (g *functionGenerator) genNext(inst *ssa.Next) (insts []wat.Inst, ret_type wir.ValueType) {
	if inst.IsString {
		iter := g.getValue(inst.Iter).value
		return g.module.EmitGenNext_String(iter)
	} else {
		logger.Fatalf("Todo:%T", inst.Type())
	}
	return
}

func (g *functionGenerator) addRegister(typ wir.ValueType) wir.Value {
	defer func() { g.cur_local_id++ }()
	name := "$t" + strconv.Itoa(g.cur_local_id)
	v := wir.NewLocal(name, typ)
	g.registers = append(g.registers, v)
	return v
}

func (g *functionGenerator) genGetter(f *ssa.Function) *wir.Function {
	var wir_fn wir.Function
	if len(f.LinkName()) > 0 {
		wir_fn.InternalName = f.LinkName()
		wir_fn.ExternalName = f.LinkName()
	} else {
		wir_fn.InternalName, wir_fn.ExternalName = wir.GetFnMangleName(f)
	}

	rets := f.Signature.Results()
	if rets.Len() > 1 {
		logger.Fatal("rets.Len() > 1")
		return nil
	}
	rtype := g.tLib.compile(rets)
	wir_fn.Results = append(wir_fn.Results, rtype)

	if len(f.Params) != 1 {
		logger.Fatal("len(f.Params) != 1")
		return nil
	}
	if !g.tLib.compile(f.Params[0].Type()).Equal(g.module.U32) {
		logger.Fatal("addr_type != U32")
		return nil
	}
	addr := wir.NewLocal("addr", g.module.GenValueType_Ptr(rtype))
	wir_fn.Params = append(wir_fn.Params, addr)

	insts, _ := g.module.EmitLoad(addr)
	wir_fn.Insts = append(wir_fn.Insts, insts...)

	return &wir_fn
}

func (g *functionGenerator) genSetter(f *ssa.Function) *wir.Function {
	var wir_fn wir.Function
	if len(f.LinkName()) > 0 {
		wir_fn.InternalName = f.LinkName()
		wir_fn.ExternalName = f.LinkName()
	} else {
		wir_fn.InternalName, wir_fn.ExternalName = wir.GetFnMangleName(f)
	}

	rets := f.Signature.Results()
	if rets.Len() > 0 {
		logger.Fatal("rets.Len() > 0")
		return nil
	}

	if len(f.Params) != 2 {
		logger.Fatal("len(f.Params) != 2")
		return nil
	}
	if !g.tLib.compile(f.Params[0].Type()).Equal(g.module.U32) {
		logger.Fatal("addr_type != U32")
		return nil
	}

	value_type := g.tLib.compile(f.Params[1].Type())

	addr := wir.NewLocal("addr", g.module.GenValueType_Ptr(value_type))
	wir_fn.Params = append(wir_fn.Params, addr)

	value := wir.NewLocal("data", value_type)
	wir_fn.Params = append(wir_fn.Params, value)

	insts := g.module.EmitStore(addr, value, false)
	wir_fn.Insts = append(wir_fn.Insts, insts...)

	return &wir_fn
}

func (g *functionGenerator) genSizer(f *ssa.Function) *wir.Function {
	var wir_fn wir.Function
	if len(f.LinkName()) > 0 {
		wir_fn.InternalName = f.LinkName()
		wir_fn.ExternalName = f.LinkName()
	} else {
		wir_fn.InternalName, wir_fn.ExternalName = wir.GetFnMangleName(f)
	}

	rets := f.Signature.Results()
	if rets.Len() != 1 {
		logger.Fatal("rets.Len() != 1")
		return nil
	}
	rtype := g.tLib.compile(rets)
	wir_fn.Results = append(wir_fn.Results, rtype)

	if len(f.Params) != 1 {
		logger.Fatal("len(f.Params) != 1")
		return nil
	}
	value_type := g.tLib.compile(f.Params[0].Type())
	t_size := value_type.(*wir.Ref).Base.Size()

	wir_fn.Insts = append(wir_fn.Insts, wir.NewConst(strconv.Itoa(t_size), g.module.I32).EmitPush()...)

	return &wir_fn
}
