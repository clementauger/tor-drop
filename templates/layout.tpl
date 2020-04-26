{{define "layout"}}
  <html>
  <head>
    <title>{{template "title" .}}</title>
    <link rel="stylesheet" href="/assets/normalize.css" />
  </head>
  <body>
    <h1><a href="{{urlFor "index"}}">tor-drop</a></h1>
    {{template "body" .}}
  </body>
  </html>
{{end}}
