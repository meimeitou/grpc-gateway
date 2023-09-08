package gengateway

import "text/template"

func (b binding) RequestHttpBody() bool {
	return b.Method.RequestType.FQMN() == ".google.api.HttpBody"
}

var (
	// 请求 body为 stream google.api.HttpBody 视为上传文件操作
	_ = template.Must(handlerTemplate.New("client-streaming-httpbody").Parse(`
{{template "request-func-signature" .}} {
	var svmetadata runtime.ServerMetadata
	const MAX_UPLOAD_SIZE = 1024 * 1024 * 100 // 100MB
	if req.ContentLength > MAX_UPLOAD_SIZE {
		return nil, svmetadata, status.Errorf(codes.InvalidArgument, "%v", "upload file max size 30MB")
	}
	file, fileHeader, err := req.FormFile("attachment")
	if err != nil {
		grpclog.Infof("Failed to start http body: %v", err)
		return nil, svmetadata, err
	}
	defer file.Close()
	// add grpc svmetadata
	var mddata metadata.MD
	mddata,exist:=metadata.FromOutgoingContext(ctx)
	if !exist {
        mddata = metadata.MD{}
	}
	mddata.Append("filename", fileHeader.Filename)
	mddata.Append("file-content-type", fileHeader.Header.Get("Content-Type"))
	mddata.Append("file-content-disposition", fileHeader.Header.Get("Content-Disposition"))
	stream, err := client.{{.Method.GetName}}(metadata.NewOutgoingContext(ctx, mddata))
	if err != nil {
		grpclog.Infof("Failed to start streaming: %v", err)
		return nil, svmetadata, err
	}
	for {
		buff := make([]byte, 32768) // 32k optimal: 16k-64k
		rlen, err := file.Read(buff)
		if err == io.EOF {
			break
		}
		if err != nil {
			grpclog.Infof("Failed to read streaming: %v", err)
			return nil, svmetadata, err
		}
		var protoReq {{.Method.RequestType.GoType .Method.Service.File.GoPkg.Path}}
		protoReq.Data = buff[:rlen]
		if err := stream.Send(&protoReq); err != nil {
			if err == io.EOF {
				break
			}
			grpclog.Infof("Failed to sed request streaming: %v", err)
			return nil, svmetadata, err
		}
	}
	if err := stream.CloseSend(); err != nil {
		grpclog.Infof("Failed to terminate client stream: %v", err)
		return nil, svmetadata, err
	}
	header, err := stream.Header()
	if err != nil {
		grpclog.Infof("Failed to get header from client: %v", err)
		return nil, svmetadata, err
	}
	svmetadata.HeaderMD = header

{{if .Method.GetServerStreaming}}
	return stream, svmetadata, nil
{{else}}
	msg, err := stream.CloseAndRecv()
	svmetadata.TrailerMD = stream.Trailer()
	return msg, svmetadata, err
{{end}}
}
	`))

	// 请求 body为 google.api.HttpBody 视为raw body传递到下游
	_ = template.Must(handlerTemplate.New("client-raw-httpbody").Parse(`
{{template "request-func-signature" .}} {
	var protoReq {{.Method.RequestType.GoType .Method.Service.File.GoPkg.Path}}
	var metadata runtime.ServerMetadata
	b, err := io.ReadAll(req.Body)
	if err != nil{
		return nil, metadata, status.Errorf(codes.InvalidArgument, "read raw http body: %v", err)
	}
	protoReq=httpbody.HttpBody{
		Data: b,
		ContentType: req.Header.Get("content-type"),
	}
	msg, err := client.{{.Method.GetName}}(ctx, &protoReq, grpc.Header(&metadata.HeaderMD), grpc.Trailer(&metadata.TrailerMD))
	return msg, metadata, err
}
	`))
)
