{{define "title"}}tor-drop home{{end}}

{{define "body"}}
  <h2>
    {{if .IsAdmin}}
    Welcome to the administrator zone
    {{else}}
    Welcome to the public zone
    {{end}}
  </h2>

  {{if .IsAdmin}}
  <a href="{{urlFor "create-folder"}}">
    <button>
      Create folder
    </button>
  </a>
  {{end}}

  {{if not (len .Folders)}}
    <br/>
    <br/>
    No folder configured yet!
  {{else}}
    <h3>Folders</h3>
    <table>
      <tr>
        <td>Name</td>
        <td>Since</td>
        {{if .IsAdmin}}
        <td>Public</td>
        <td>Edit</td>
        <td>Remove</td>
        {{end}}
      </tr>
      {{range $f := .Folders}}
      <tr>
        <td><a href="{{urlFor "folder-listing" "folder" $f.Name}}">{{$f.Name}}</a></td>
        <td>{{$f.CreateDate | times}}</td>
        {{if $.IsAdmin}}
        <td>{{if $f.IsPrivate}}no{{else}}yes{{end}}</td>
        <td><a href="{{urlFor "folder-edit" "folder" $f.Name}}">edit</a></td>
        <td>
          <form method="POST" action="{{urlFor "folder-rm" "folder" $f.Name}}">
            {{$.Request | csrf}}
            <input type="hidden" name="Name" value="{{$f.Name}}" />
            <button type="submit">{{$f.Name}}</button>
          </form>
        </td>
        {{end}}
      </tr>
      {{end}}
    </table>
  {{end}}

{{end}}

{{template "layout" .}}
