// Code generated by protoc-gen-go. DO NOT EDIT.
// source: cdprocessor.proto

/*
Package cdprocessor is a generated protocol buffer package.

It is generated from these files:
	cdprocessor.proto

It has these top-level messages:
	GetRippedRequest
	GetRippedResponse
	GetMissingRequest
	GetMissingResponse
*/
package cdprocessor

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"
import recordcollection "github.com/brotherlogic/recordcollection/proto"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

type GetRippedRequest struct {
}

func (m *GetRippedRequest) Reset()                    { *m = GetRippedRequest{} }
func (m *GetRippedRequest) String() string            { return proto.CompactTextString(m) }
func (*GetRippedRequest) ProtoMessage()               {}
func (*GetRippedRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{0} }

type GetRippedResponse struct {
	RippedIds []int32 `protobuf:"varint,1,rep,packed,name=ripped_ids,json=rippedIds" json:"ripped_ids,omitempty"`
}

func (m *GetRippedResponse) Reset()                    { *m = GetRippedResponse{} }
func (m *GetRippedResponse) String() string            { return proto.CompactTextString(m) }
func (*GetRippedResponse) ProtoMessage()               {}
func (*GetRippedResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{1} }

func (m *GetRippedResponse) GetRippedIds() []int32 {
	if m != nil {
		return m.RippedIds
	}
	return nil
}

type GetMissingRequest struct {
}

func (m *GetMissingRequest) Reset()                    { *m = GetMissingRequest{} }
func (m *GetMissingRequest) String() string            { return proto.CompactTextString(m) }
func (*GetMissingRequest) ProtoMessage()               {}
func (*GetMissingRequest) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{2} }

type GetMissingResponse struct {
	Missing []*recordcollection.Record `protobuf:"bytes,1,rep,name=missing" json:"missing,omitempty"`
}

func (m *GetMissingResponse) Reset()                    { *m = GetMissingResponse{} }
func (m *GetMissingResponse) String() string            { return proto.CompactTextString(m) }
func (*GetMissingResponse) ProtoMessage()               {}
func (*GetMissingResponse) Descriptor() ([]byte, []int) { return fileDescriptor0, []int{3} }

func (m *GetMissingResponse) GetMissing() []*recordcollection.Record {
	if m != nil {
		return m.Missing
	}
	return nil
}

func init() {
	proto.RegisterType((*GetRippedRequest)(nil), "cdprocessor.GetRippedRequest")
	proto.RegisterType((*GetRippedResponse)(nil), "cdprocessor.GetRippedResponse")
	proto.RegisterType((*GetMissingRequest)(nil), "cdprocessor.GetMissingRequest")
	proto.RegisterType((*GetMissingResponse)(nil), "cdprocessor.GetMissingResponse")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for CDProcessor service

type CDProcessorClient interface {
	GetRipped(ctx context.Context, in *GetRippedRequest, opts ...grpc.CallOption) (*GetRippedResponse, error)
	GetMissing(ctx context.Context, in *GetMissingRequest, opts ...grpc.CallOption) (*GetMissingResponse, error)
}

type cDProcessorClient struct {
	cc *grpc.ClientConn
}

func NewCDProcessorClient(cc *grpc.ClientConn) CDProcessorClient {
	return &cDProcessorClient{cc}
}

func (c *cDProcessorClient) GetRipped(ctx context.Context, in *GetRippedRequest, opts ...grpc.CallOption) (*GetRippedResponse, error) {
	out := new(GetRippedResponse)
	err := grpc.Invoke(ctx, "/cdprocessor.CDProcessor/GetRipped", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cDProcessorClient) GetMissing(ctx context.Context, in *GetMissingRequest, opts ...grpc.CallOption) (*GetMissingResponse, error) {
	out := new(GetMissingResponse)
	err := grpc.Invoke(ctx, "/cdprocessor.CDProcessor/GetMissing", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Server API for CDProcessor service

type CDProcessorServer interface {
	GetRipped(context.Context, *GetRippedRequest) (*GetRippedResponse, error)
	GetMissing(context.Context, *GetMissingRequest) (*GetMissingResponse, error)
}

func RegisterCDProcessorServer(s *grpc.Server, srv CDProcessorServer) {
	s.RegisterService(&_CDProcessor_serviceDesc, srv)
}

func _CDProcessor_GetRipped_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetRippedRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CDProcessorServer).GetRipped(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cdprocessor.CDProcessor/GetRipped",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CDProcessorServer).GetRipped(ctx, req.(*GetRippedRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CDProcessor_GetMissing_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetMissingRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CDProcessorServer).GetMissing(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cdprocessor.CDProcessor/GetMissing",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CDProcessorServer).GetMissing(ctx, req.(*GetMissingRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _CDProcessor_serviceDesc = grpc.ServiceDesc{
	ServiceName: "cdprocessor.CDProcessor",
	HandlerType: (*CDProcessorServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "GetRipped",
			Handler:    _CDProcessor_GetRipped_Handler,
		},
		{
			MethodName: "GetMissing",
			Handler:    _CDProcessor_GetMissing_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "cdprocessor.proto",
}

func init() { proto.RegisterFile("cdprocessor.proto", fileDescriptor0) }

var fileDescriptor0 = []byte{
	// 237 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x74, 0x90, 0xcf, 0x4a, 0xc3, 0x40,
	0x10, 0xc6, 0x29, 0xa2, 0xd2, 0xc9, 0xc5, 0xae, 0x97, 0x12, 0xa8, 0x4a, 0x4e, 0x9e, 0x12, 0x88,
	0x8f, 0xa0, 0xe2, 0x1f, 0x28, 0xc8, 0xbe, 0x80, 0x90, 0xdd, 0x21, 0x5d, 0x48, 0x33, 0xeb, 0xcc,
	0xf6, 0x9d, 0x7c, 0x4c, 0x61, 0xd3, 0xe8, 0xda, 0x90, 0xeb, 0x6f, 0x66, 0x7e, 0x7c, 0xdf, 0xc0,
	0xca, 0x58, 0xcf, 0x64, 0x50, 0x84, 0xb8, 0xf4, 0x4c, 0x81, 0x54, 0x96, 0xa0, 0xfc, 0xb9, 0x75,
	0x61, 0x77, 0x68, 0x4a, 0x43, 0xfb, 0xaa, 0x61, 0x0a, 0x3b, 0xe4, 0x8e, 0x5a, 0x67, 0x2a, 0x46,
	0x43, 0x6c, 0x0d, 0x75, 0x1d, 0x9a, 0xe0, 0xa8, 0xaf, 0xe2, 0xf1, 0x04, 0x0f, 0xce, 0x42, 0xc1,
	0xd5, 0x0b, 0x06, 0xed, 0xbc, 0x47, 0xab, 0xf1, 0xeb, 0x80, 0x12, 0x8a, 0x1a, 0x56, 0x09, 0x13,
	0x4f, 0xbd, 0xa0, 0xda, 0x00, 0x70, 0x24, 0x9f, 0xce, 0xca, 0x7a, 0x71, 0x77, 0x76, 0x7f, 0xae,
	0x97, 0x03, 0x79, 0xb3, 0x52, 0x5c, 0xc7, 0x9b, 0xad, 0x13, 0x71, 0x7d, 0x3b, 0x8a, 0x5e, 0x41,
	0xa5, 0xf0, 0x68, 0xaa, 0xe1, 0x72, 0x3f, 0xa0, 0xa8, 0xc9, 0xea, 0x75, 0x39, 0x09, 0xa7, 0x23,
	0xd0, 0xe3, 0x62, 0xfd, 0xbd, 0x80, 0xec, 0xf1, 0xe9, 0x63, 0x6c, 0xaf, 0xde, 0x61, 0xf9, 0x1b,
	0x51, 0x6d, 0xca, 0xf4, 0x57, 0xa7, 0x75, 0xf2, 0x9b, 0xb9, 0xf1, 0x31, 0xcf, 0x16, 0xe0, 0x2f,
	0xa5, 0x9a, 0x6c, 0xff, 0xef, 0x94, 0xdf, 0xce, 0xce, 0x07, 0x5d, 0x73, 0x11, 0x1f, 0xfb, 0xf0,
	0x13, 0x00, 0x00, 0xff, 0xff, 0x2d, 0xad, 0x5c, 0x15, 0xc1, 0x01, 0x00, 0x00,
}
