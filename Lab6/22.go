{{define "news"}}
    {{range .}}
        <div class="news-item">
            <h2><a href="{{.Link}}" target="_blank">{{.Title}}</a></h2>
            <p>{{.Description}}</p>
            <small>{{.Date.Format "02.01.2006"}}</small>
            <hr>
        </div>
    {{end}}
{{end}}
