{{define "newsItem"}}
<div>
    <h2>{{.Title}}</h2>
    <p><em>{{.Date}}</em></p>
    <p>{{.Content}}</p>
    <p><a href="{{.Link}}">Read more</a></p>
    <hr>
</div>
{{end}}
