// 版权 @2019 凹语言 作者。保留所有权利。

package appllvm

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"wa-lang.org/wa/internal/3rdparty/cli"
	"wa-lang.org/wa/internal/app/appbase"
	"wa-lang.org/wa/internal/backends/compiler_llvm"
	"wa-lang.org/wa/internal/config"
	"wa-lang.org/wa/internal/loader"
)

var CmdNative = &cli.Command{
	Hidden: true,
	Name:   "native",
	Usage:  "compile Wa source code to native executable",
	Flags: []cli.Flag{
		appbase.MakeFlag_output(),
		appbase.MakeFlag_target(),
		appbase.MakeFlag_tags(),

		&cli.BoolFlag{
			Name:  "debug",
			Usage: "dump orginal intermediate representation",
		},
		&cli.StringFlag{
			Name:  "clang",
			Usage: "set llvm/clang path",
		},
		&cli.StringFlag{
			Name:  "llc",
			Usage: "set llvm/llc path",
		},
	},
	Action: func(c *cli.Context) error {
		outfile := c.String("output")
		target := c.String("target")
		debug := c.Bool("debug")
		infile := ""

		if c.NArg() == 0 {
			fmt.Fprintf(os.Stderr, "no input file")
			os.Exit(1)
		}
		infile = c.Args().First()

		opt := appbase.BuildOptions(c, config.WaBackend_llvm)
		if err := LLVMRun(opt, infile, outfile, target, debug); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		return nil
	},
}

func LLVMRun(opt *appbase.Option, infile string, outfile string, target string, debug bool) error {
	cfg := opt.Config()

	instat, err := os.Stat(infile)
	if err != nil {
		return err
	}

	// Calculate the outfile path if not given.
	if len(outfile) == 0 {
		if instat.IsDir() {
			dir := path.Base(infile)
			outfile = infile + dir + ".exe"
		} else {
			ext := path.Ext(infile)
			if len(ext) == 0 {
				outfile = infile + ".exe"
			} else {
				pos := strings.Index(infile, ext)
				outfile = infile[0:pos] + ".exe"
			}
		}
	}

	// Calculate the outfile LLVM-IR file path and the output assembly file path.
	llfile, asmfile := "", ""
	ext := path.Ext(outfile)
	if len(ext) == 0 {
		llfile = outfile + ".ll"
		asmfile = outfile + ".s"
	} else {
		pos := strings.Index(outfile, ext)
		llfile = outfile[0:pos] + ".ll"
		asmfile = outfile[0:pos] + ".s"
	}

	// Do the real compile work.
	prog, err := loader.LoadProgram(cfg, infile)
	if err != nil {
		return err
	}
	output, err := compiler_llvm.New(target, debug).Compile(prog)
	if err != nil {
		return err
	}

	// Write the outfile LLVM-IR to an intermediate .ll file.
	if err := os.WriteFile(llfile, []byte(output), 0644); err != nil {
		return err
	}

	// Invoke command `llc xxx.ll -mtriple=xxx`.
	llc := []string{llfile}
	if target != "" {
		llc = append(llc, "-mtriple", target)
	}
	// Add target specific options.
	switch target {
	case "avr":
		llc = append(llc, "-mcpu=atmega328")
	default:
	}
	cmd0 := exec.Command(opt.Llc, llc...)
	cmd0.Stderr = os.Stderr
	if err := cmd0.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "**** failed to invoke LLVM ****\n")
		return err
	}

	// TODO: This is a temporary solution for AVR-Arduino. We generate
	// an Arduino project instead of an ELF.
	if target == "avr" {
		// Create a new directory for the output Arduino project.
		ext, outdir := path.Ext(outfile), ""
		if len(ext) > 0 {
			pos := strings.Index(outfile, ext)
			outdir = outfile[0:pos]
		}
		if err := os.RemoveAll(outdir); err != nil {
			return err
		}
		if err := os.Mkdir(outdir, 0755); err != nil {
			return err
		}
		// Move the assembly file to the project directory.
		newAsmFile := strings.ReplaceAll(asmfile, ".s", ".S")
		if err := os.Rename(asmfile, path.Join(outdir, newAsmFile)); err != nil {
			return err
		}
		// Create the project main '.ino' file.
		inoFile := path.Join(outdir, path.Base(outdir)+".ino")
		inoStr := "void setup(void) {}\nextern \"C\" { extern void wa_main(void); }\nvoid loop(void) { wa_main(); }\n"
		if err := os.WriteFile(inoFile, []byte(inoStr), 0644); err != nil {
			return err
		}
		return nil
	}

	// Invoke command `clang xxx.s -o outfile --target=xxx`.
	clangArgs := []string{asmfile, "-static", "-o", outfile}
	if target != "" {
		clangArgs = append(clangArgs, "-target", target)
	}
	if opt.Debug {
		clangArgs = append(clangArgs, "-v")
	}
	cmd1 := exec.Command(opt.Clang, clangArgs...)
	cmd1.Stderr = os.Stderr
	cmd1.Stdout = os.Stdout
	if err := cmd1.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "**** failed to invoke CLANG ****\n")
		return err
	}

	return nil
}
