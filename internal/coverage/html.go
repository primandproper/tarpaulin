package coverage

import (
	"html/template"
)

// pageTemplate renders the whole report. The layout follows `go tool cover
// -html` as it stands today — the fixed topbar, the file picker, one hidden
// <pre> per file, and the fragment-driven switch — because the point of the
// report is the coloring, and a familiar page is one less thing to learn.
var pageTemplate = template.Must(template.New("coverage").Parse(pageHTML))

const pageHTML = `<!DOCTYPE html>
<html lang="en">
	<head>
		<meta charset="utf-8">
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<title>{{if .Package}}{{.Package}}: {{end}}tarp coverage</title>
		<style>
			body {
				background: rgb(16, 16, 16);
				color: rgb(110, 110, 110);
			}
			body, pre, #legend span, #grade {
				font-family: Menlo, monospace;
				font-weight: bold;
			}
			#topbar {
				background: rgb(16, 16, 16);
				position: fixed;
				top: 0; left: 0; right: 0;
				height: 42px;
				border-bottom: 1px solid rgb(80, 80, 80);
			}
			#content {
				margin-top: 50px;
			}
			#nav, #legend {
				float: left;
				margin-left: 10px;
			}
			#grade {
				float: right;
				margin-top: 12px;
				margin-right: 10px;
				color: rgb(200, 200, 200);
			}
			#legend {
				margin-top: 12px;
			}
			#nav {
				margin-top: 10px;
			}
			#legend span {
				margin: 0 5px;
			}
			.tarp-uncovered { color: rgb(214, 60, 60) }
			.tarp-indirect  { color: rgb(252, 242, 106) }
			.tarp-direct    { color: rgb(60, 202, 96) }
			.tarp-ungraded  { color: rgb(150, 150, 150) }
		</style>
	</head>
	<body>
		<div id="topbar">
			<div id="nav">
				<select id="files">
				{{range $i, $f := .Files}}
				<option value="file{{$i}}">{{$f.Name}} ({{printf "%.1f" $f.Coverage}}% covered, {{$f.Tested}}/{{$f.Declared}} tested)</option>
				{{end}}
				</select>
			</div>
			<div id="legend">
				<span>not tracked</span>
				<span class="tarp-uncovered">never ran</span>
				<span class="tarp-indirect">ran, no direct test</span>
				<span class="tarp-direct">directly tested</span>
				<span class="tarp-ungraded">not graded</span>
			</div>
			<div id="grade">Grade: {{.Score}}% ({{.Tested}}/{{.Declared}} functions)</div>
		</div>
		<div id="content">
		{{range $i, $f := .Files}}
		<pre class="file" id="file{{$i}}" style="display: none">{{$f.Body}}</pre>
		{{end}}
		</div>
		<script>
		(function() {
			var files = document.getElementById('files');
			var visible;
			files.addEventListener('change', onChange, false);
			function select(part) {
				if (visible)
					visible.style.display = 'none';
				visible = document.getElementById(part);
				if (!visible)
					return;
				files.value = part;
				visible.style.display = 'block';
				location.hash = part;
			}
			function onChange() {
				select(files.value);
				window.scrollTo(0, 0);
			}
			if (location.hash != "") {
				select(location.hash.substr(1));
			}
			if (!visible) {
				select("file0");
			}
		})();
		</script>
	</body>
</html>
`
