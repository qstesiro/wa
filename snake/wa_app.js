(() => {
  class WaApp {
    constructor() {
      this._inst = null;
      this._wa_print_buf = "";
    }

    init(url) {
      let app = this;
      let importsObject = {
        wasi_snapshot_preview1: new function () {
            this.args_get = () => { return 0; }
            this.args_sizes_get = () => { return 0; }

			this.clock_res_get = () => { return 0; }
			this.clock_time_get = () => { return 0; }
			this.environ_get = () => { return 0; }
			this.environ_sizes_get = () => { return 0; }

			this.fd_advise = () => { return 0; }
			this.fd_allocate = () => { return 0; }
			this.fd_close = () => { return 0; }
			this.fd_datasync = () => { return 0; }
			this.fd_fdstat_get = () => { return 0; }
			this.fd_fdstat_set_flags = () => { return 0; }
			this.fd_fdstat_set_rights = () => { return 0; }
			this.fd_filestat_get = () => { return 0; }
			this.fd_filestat_set_size = () => { return 0; }
			this.fd_filestat_set_times = () => { return 0; }
			this.fd_pread = () => { return 0; }
			this.fd_prestat_get = () => { return 0; }
			this.fd_prestat_dir_name = () => { return 0; }
			this.fd_pwrite = () => { return 0; }
			this.fd_read = () => { return 0; }
			this.fd_readdir = () => { return 0; }
			this.fd_renumber = () => { return 0; }
			this.fd_seek = () => { return 0; }
			this.fd_sync = () => { return 0; }
			this.fd_tell = () => { return 0; }
			this.fd_write = () => { return 0; }
			this.path_create_directory = () => { return 0; }
			this.path_filestat_get = () => { return 0; }
			this.path_filestat_set_times = () => { return 0; }
			this.path_link = () => { return 0; }
			this.path_open = () => { return 0; }
			this.path_readlink = () => { return 0; }
			this.path_remove_directory = () => { return 0; }
			this.path_rename = () => { return 0; }
			this.path_symlink = () => { return 0; }
			this.path_unlink_file = () => { return 0; }
			
			this.poll_oneoff = () => { return 0; }
			this.proc_exit = () => { return 0; }
			this.random_get = () => { return 0; }
			this.sched_yield = () => { return 0; }
			this.sock_accept = () => { return 0; }
			this.sock_recv = () => { return 0; }
			this.sock_send = () => { return 0; }
			this.sock_shutdown = () => { return 0; }
        },
        wa_js_env: new function () {
          this.waPrintI32 = (i) => {
            app._wa_print_buf += i
          }

          this.waPrintRune = (c) => {
            let ch = String.fromCodePoint(c);
            if (ch == '\n') {
              console.log(app._wa_print_buf);
              app._wa_print_buf = "";
            }
            else {
              app._wa_print_buf += ch
            }
          }

          this.waPuts = (prt, len) => {
            let s = app.getString(prt, len);
            app._wa_print_buf += s
          }

          this.rand = (m) => {
            return parseInt(Math.random() * m)
          }

          this.newCanvas = (w, h) => {
            let canvas = document.createElement('canvas');
            canvas.width = w;
            canvas.height = h;
            canvas.id = 0;  //!!!!!

            const waContent = document.getElementById('game__screen-content');
            waContent.appendChild(canvas);

            function getPointOnCanvas(x, y) {
              var bbox = canvas.getBoundingClientRect();
              return {
                x: parseInt((x - bbox.left) * (canvas.width / bbox.width)),
                y: parseInt((y - bbox.top) * (canvas.height / bbox.height))
              };
            }

            function onMouseDown(ev) {
              let pt = getPointOnCanvas(ev.clientX, ev.clientY);
              app._inst.exports['snake$canvas.OnMouseDown'](canvas.id, pt.x, pt.y);
            }

            function onMouseUp(ev) {
              let pt = getPointOnCanvas(ev.clientX, ev.clientY);
              app._inst.exports['snake$canvas.OnMouseUp'](canvas.id, pt.x, pt.y);
            }

            function onKeyDown(ev) {
              app._inst.exports['snake$canvas.OnKeyDown'](canvas.id, ev.keyCode);
            }

            function onKeyUp(ev) {
              app._inst.exports['snake$canvas.OnKeyUp'](canvas.id, ev.keyCode);
            }

            if (IS_MOBILE) {
              MOBILE_DIR_MAP.forEach((dir) => {
                const el = document.getElementById(dir.id);
                el.addEventListener('touchstart', (ev) => onKeyDown({ keyCode: dir.keyCode }));
                el.addEventListener('touchend', (ev) => onKeyUp({ keyCode: dir.keyCode }));
              });
            }

            canvas.addEventListener('mousedown', onMouseDown, true);
            canvas.addEventListener('mouseup', onMouseUp, true);
            canvas.addEventListener('keydown', onKeyDown, true);
            canvas.addEventListener('keyup', onKeyUp, true);
            canvas.tabIndex = -1;  //tabindex
            canvas.focus();

            this._ctx = canvas.getContext('2d');
            this._canvas = canvas;
            return canvas.id;
          }
          this.updateCanvas = (id, block, data) => {
            let img = this._ctx.createImageData(this._canvas.width, this._canvas.height);
            let buf_len = this._canvas.width * this._canvas.height * 4
            let buf = app.memUint8Array(data, buf_len);
            for (var i = 0; i < buf_len; i++) {
              img.data[i] = buf[i];
            }
            this._ctx.putImageData(img, 0, 0);
          }
        }
      }
      WebAssembly.instantiateStreaming(fetch(url), importsObject).then(res => {
        this._inst = res.instance;
        this._inst.exports._start();
        const timer = setInterval(gameLoop, 150);
      })
    }

    mem() {
      return this._inst.exports.memory;
    }

    memView(addr, len) {
      return new DataView(this._inst.exports.memory.buffer, addr, len);
    }

    memUint8Array(addr, len) {
      return new Uint8Array(this.mem().buffer, addr, len)
    }

    getString(addr, len) {
      return new TextDecoder("utf-8").decode(this.memView(addr, len));
    }

    setString(addr, len, s) {
      const bytes = new TextEncoder("utf-8").encode(s);
      if (len > bytes.length) { len = bytes.length; }
      this.MemUint8Array(addr, len).set(bytes);
    }
  }

  function gameLoop() {
    window['waApp']._inst.exports['snake.Step']();
  }

  window['waApp'] = new WaApp();
  window['waApp'].init("./snake.wasm")
})()