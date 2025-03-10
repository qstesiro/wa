// 版权 @2023 凹语言 作者。保留所有权利。

package appbuild

import (
	"fmt"
	"os"
	"path/filepath"

	"wa-lang.org/wa/internal/3rdparty/cli"
	"wa-lang.org/wa/internal/app/appbase"
	"wa-lang.org/wa/internal/backends/compiler_wat"
	"wa-lang.org/wa/internal/config"
	"wa-lang.org/wa/internal/loader"
	"wa-lang.org/wa/internal/wabt"
)

var CmdBuild = &cli.Command{
	Name:  "build",
	Usage: "compile Wa source code",
	Flags: []cli.Flag{
		appbase.MakeFlag_output(),
		appbase.MakeFlag_target(),
		appbase.MakeFlag_tags(),
		appbase.MakeFlag_ld_stack_size(),
		appbase.MakeFlag_ld_max_memory(),
	},
	Action: CmdBuildAction,
}

func CmdBuildAction(c *cli.Context) error {
	input := c.Args().First()
	outfile := ""

	if input == "" {
		input, _ = os.Getwd()
	}

	var opt = appbase.BuildOptions(c)
	_, err := BuildApp(opt, input, outfile)
	if err != nil {
		return err
	}
	return err
}

func BuildApp(opt *appbase.Option, input, outfile string) (wasmBytes []byte, err error) {
	// 路径是否存在
	if !appbase.PathExists(input) {
		fmt.Printf("%q not found\n", input)
		os.Exit(1)
	}

	// 输出参数是否合法, 必须是 wasm
	if outfile != "" && !appbase.HasExt(outfile, ".wasm") {
		fmt.Printf("%q is not valid output path\n", outfile)
		os.Exit(1)
	}

	// 已经是 wasm, 直接返回
	if appbase.HasExt(input, ".wasm") {
		return os.ReadFile(input)
	}

	// 只编译 wat 文件, 输出路径相同, 后缀名调整
	if appbase.HasExt(input, ".wat") {
		// 设置默认输出目标
		if outfile == "" {
			outfile = appbase.ReplaceExt(input, ".wat", ".wasm")
		}

		watData, err := os.ReadFile(input)
		if err != nil {
			fmt.Printf("read %s failed: %v\n", input, err)
			os.Exit(1)
		}
		wasmBytes, err := wabt.Wat2Wasm(watData)
		if err != nil {
			fmt.Printf("wat2wasm %s failed: %v\n", input, err)
			os.Exit(1)
		}

		// 写到文件
		err = os.WriteFile(outfile, wasmBytes, 0666)
		if err != nil {
			fmt.Printf("write %s failed: %v\n", outfile, err)
			os.Exit(1)
		}

		// OK
		return wasmBytes, nil
	}

	// 只编译 wa/wz 文件, 输出路径相同, 后缀名调整
	if appbase.HasExt(input, ".wa", ".wz") {
		_, watOutput, err := buildWat(opt, input)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// 设置默认输出目标
		if outfile == "" {
			outfile = appbase.ReplaceExt(input, ".wa", ".wasm")
		}

		// wat 写到文件
		watOutfile := appbase.ReplaceExt(outfile, ".wasm", ".wat")
		err = os.WriteFile(watOutfile, watOutput, 0666)
		if err != nil {
			fmt.Printf("write %s failed: %v\n", outfile, err)
			os.Exit(1)
		}

		// wat 编译为 wasm
		wasmBytes, err := wabt.Wat2Wasm(watOutput)
		if err != nil {
			fmt.Printf("wat2wasm %s failed: %v\n", input, err)
			os.Exit(1)
		}

		// wasm 写到文件
		err = os.WriteFile(outfile, wasmBytes, 0666)
		if err != nil {
			fmt.Printf("write %s failed: %v\n", outfile, err)
			os.Exit(1)
		}

		// OK
		return wasmBytes, nil
	}

	// 构建目录
	{
		if !appbase.IsNativeDir(input) {
			fmt.Printf("%q is not valid output path\n", outfile)
			os.Exit(1)
		}

		// 尝试读取模块信息
		manifest, err := config.LoadManifest(nil, input)
		if err != nil {
			fmt.Printf("%q is invalid wa moudle\n", input)
			os.Exit(1)
		}
		if err := manifest.Valid(); err != nil {
			fmt.Printf("%q is invalid wa module; %v\n", input, err)
			os.Exit(1)
		}

		if outfile == "" {
			if !manifest.IsStd {
				outfile = filepath.Join(manifest.Root, "output", manifest.Pkg.Name) + ".wasm"
				os.MkdirAll(filepath.Join(manifest.Root, "output"), 0777)
			} else {
				outfile = "a.out." + manifest.Pkg.Name + ".wasm"
			}
		}

		// 编译出 wat 文件
		_, watOutput, err := buildWat(opt, input)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// wat 写到文件
		watOutfile := appbase.ReplaceExt(outfile, ".wasm", ".wat")
		err = os.WriteFile(watOutfile, watOutput, 0666)
		if err != nil {
			fmt.Printf("write %s failed: %v\n", outfile, err)
			os.Exit(1)
		}

		// wat 编译为 wasm
		wasmBytes, err := wabt.Wat2Wasm(watOutput)
		if err != nil {
			fmt.Printf("wat2wasm %s failed: %v\n", input, err)
			os.Exit(1)
		}

		// wasm 写到文件
		err = os.WriteFile(outfile, wasmBytes, 0666)
		if err != nil {
			fmt.Printf("write %s failed: %v\n", outfile, err)
			os.Exit(1)
		}

		// OK
		return wasmBytes, nil
	}
}

func buildWat(opt *appbase.Option, filename string) (*loader.Program, []byte, error) {
	cfg := opt.Config()
	prog, err := loader.LoadProgram(cfg, filename)
	if err != nil {
		return prog, nil, err
	}

	output, err := compiler_wat.New().Compile(prog, "main")

	if err != nil {
		return prog, nil, err
	}

	return prog, []byte(output), nil
}
