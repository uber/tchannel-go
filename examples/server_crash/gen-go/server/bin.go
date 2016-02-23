// Autogenerated by Thrift Compiler (0.9.2)
// DO NOT EDIT UNLESS YOU ARE SURE THAT YOU KNOW WHAT YOU ARE DOING

package server

import (
	"bytes"
	"fmt"
	"github.com/apache/thrift/lib/go/thrift"
)

// (needed to ensure safety because of naive import list construction.)
var _ = thrift.ZERO
var _ = fmt.Printf
var _ = bytes.Equal

type BIn interface {
	// Parameters:
	//  - Messages
	OpenPublisherStream(messages *PutMessageStream) (r *PutMessageAckStream, err error)
}

type BInClient struct {
	Transport       thrift.TTransport
	ProtocolFactory thrift.TProtocolFactory
	InputProtocol   thrift.TProtocol
	OutputProtocol  thrift.TProtocol
	SeqId           int32
}

func NewBInClientFactory(t thrift.TTransport, f thrift.TProtocolFactory) *BInClient {
	return &BInClient{Transport: t,
		ProtocolFactory: f,
		InputProtocol:   f.GetProtocol(t),
		OutputProtocol:  f.GetProtocol(t),
		SeqId:           0,
	}
}

func NewBInClientProtocol(t thrift.TTransport, iprot thrift.TProtocol, oprot thrift.TProtocol) *BInClient {
	return &BInClient{Transport: t,
		ProtocolFactory: nil,
		InputProtocol:   iprot,
		OutputProtocol:  oprot,
		SeqId:           0,
	}
}

// Parameters:
//  - Messages
func (p *BInClient) OpenPublisherStream(messages *PutMessageStream) (r *PutMessageAckStream, err error) {
	if err = p.sendOpenPublisherStream(messages); err != nil {
		return
	}
	return p.recvOpenPublisherStream()
}

func (p *BInClient) sendOpenPublisherStream(messages *PutMessageStream) (err error) {
	oprot := p.OutputProtocol
	if oprot == nil {
		oprot = p.ProtocolFactory.GetProtocol(p.Transport)
		p.OutputProtocol = oprot
	}
	p.SeqId++
	if err = oprot.WriteMessageBegin("OpenPublisherStream", thrift.CALL, p.SeqId); err != nil {
		return
	}
	args := OpenPublisherStreamArgs{
		Messages: messages,
	}
	if err = args.Write(oprot); err != nil {
		return
	}
	if err = oprot.WriteMessageEnd(); err != nil {
		return
	}
	return oprot.Flush()
}

func (p *BInClient) recvOpenPublisherStream() (value *PutMessageAckStream, err error) {
	iprot := p.InputProtocol
	if iprot == nil {
		iprot = p.ProtocolFactory.GetProtocol(p.Transport)
		p.InputProtocol = iprot
	}
	_, mTypeId, seqId, err := iprot.ReadMessageBegin()
	if err != nil {
		return
	}
	if mTypeId == thrift.EXCEPTION {
		error0 := thrift.NewTApplicationException(thrift.UNKNOWN_APPLICATION_EXCEPTION, "Unknown Exception")
		var error1 error
		error1, err = error0.Read(iprot)
		if err != nil {
			return
		}
		if err = iprot.ReadMessageEnd(); err != nil {
			return
		}
		err = error1
		return
	}
	if p.SeqId != seqId {
		err = thrift.NewTApplicationException(thrift.BAD_SEQUENCE_ID, "OpenPublisherStream failed: out of sequence response")
		return
	}
	result := OpenPublisherStreamResult{}
	if err = result.Read(iprot); err != nil {
		return
	}
	if err = iprot.ReadMessageEnd(); err != nil {
		return
	}
	if result.EntityError != nil {
		err = result.EntityError
		return
	} else if result.EntityDisabled != nil {
		err = result.EntityDisabled
		return
	} else if result.RequestError != nil {
		err = result.RequestError
		return
	}
	value = result.GetSuccess()
	return
}

type BInProcessor struct {
	processorMap map[string]thrift.TProcessorFunction
	handler      BIn
}

func (p *BInProcessor) AddToProcessorMap(key string, processor thrift.TProcessorFunction) {
	p.processorMap[key] = processor
}

func (p *BInProcessor) GetProcessorFunction(key string) (processor thrift.TProcessorFunction, ok bool) {
	processor, ok = p.processorMap[key]
	return processor, ok
}

func (p *BInProcessor) ProcessorMap() map[string]thrift.TProcessorFunction {
	return p.processorMap
}

func NewBInProcessor(handler BIn) *BInProcessor {

	self2 := &BInProcessor{handler: handler, processorMap: make(map[string]thrift.TProcessorFunction)}
	self2.processorMap["OpenPublisherStream"] = &bInProcessorOpenPublisherStream{handler: handler}
	return self2
}

func (p *BInProcessor) Process(iprot, oprot thrift.TProtocol) (success bool, err thrift.TException) {
	name, _, seqId, err := iprot.ReadMessageBegin()
	if err != nil {
		return false, err
	}
	if processor, ok := p.GetProcessorFunction(name); ok {
		return processor.Process(seqId, iprot, oprot)
	}
	iprot.Skip(thrift.STRUCT)
	iprot.ReadMessageEnd()
	x3 := thrift.NewTApplicationException(thrift.UNKNOWN_METHOD, "Unknown function "+name)
	oprot.WriteMessageBegin(name, thrift.EXCEPTION, seqId)
	x3.Write(oprot)
	oprot.WriteMessageEnd()
	oprot.Flush()
	return false, x3

}

type bInProcessorOpenPublisherStream struct {
	handler BIn
}

func (p *bInProcessorOpenPublisherStream) Process(seqId int32, iprot, oprot thrift.TProtocol) (success bool, err thrift.TException) {
	args := OpenPublisherStreamArgs{}
	if err = args.Read(iprot); err != nil {
		iprot.ReadMessageEnd()
		x := thrift.NewTApplicationException(thrift.PROTOCOL_ERROR, err.Error())
		oprot.WriteMessageBegin("OpenPublisherStream", thrift.EXCEPTION, seqId)
		x.Write(oprot)
		oprot.WriteMessageEnd()
		oprot.Flush()
		return false, err
	}

	iprot.ReadMessageEnd()
	result := OpenPublisherStreamResult{}
	var retval *PutMessageAckStream
	var err2 error
	if retval, err2 = p.handler.OpenPublisherStream(args.Messages); err2 != nil {
		switch v := err2.(type) {
		case *EntityNotExistsError:
			result.EntityError = v
		case *EntityDisabledError:
			result.EntityDisabled = v
		case *BadRequestError:
			result.RequestError = v
		default:
			x := thrift.NewTApplicationException(thrift.INTERNAL_ERROR, "Internal error processing OpenPublisherStream: "+err2.Error())
			oprot.WriteMessageBegin("OpenPublisherStream", thrift.EXCEPTION, seqId)
			x.Write(oprot)
			oprot.WriteMessageEnd()
			oprot.Flush()
			return true, err2
		}
	} else {
		result.Success = retval
	}
	if err2 = oprot.WriteMessageBegin("OpenPublisherStream", thrift.REPLY, seqId); err2 != nil {
		err = err2
	}
	if err2 = result.Write(oprot); err == nil && err2 != nil {
		err = err2
	}
	if err2 = oprot.WriteMessageEnd(); err == nil && err2 != nil {
		err = err2
	}
	if err2 = oprot.Flush(); err == nil && err2 != nil {
		err = err2
	}
	if err != nil {
		return
	}
	return true, err
}

// HELPER FUNCTIONS AND STRUCTURES

type OpenPublisherStreamArgs struct {
	Messages *PutMessageStream `thrift:"messages,1" json:"messages"`
}

func NewOpenPublisherStreamArgs() *OpenPublisherStreamArgs {
	return &OpenPublisherStreamArgs{}
}

var OpenPublisherStreamArgs_Messages_DEFAULT *PutMessageStream

func (p *OpenPublisherStreamArgs) GetMessages() *PutMessageStream {
	if !p.IsSetMessages() {
		return OpenPublisherStreamArgs_Messages_DEFAULT
	}
	return p.Messages
}
func (p *OpenPublisherStreamArgs) IsSetMessages() bool {
	return p.Messages != nil
}

func (p *OpenPublisherStreamArgs) Read(iprot thrift.TProtocol) error {
	if _, err := iprot.ReadStructBegin(); err != nil {
		return fmt.Errorf("%T read error: %s", p, err)
	}
	for {
		_, fieldTypeId, fieldId, err := iprot.ReadFieldBegin()
		if err != nil {
			return fmt.Errorf("%T field %d read error: %s", p, fieldId, err)
		}
		if fieldTypeId == thrift.STOP {
			break
		}
		switch fieldId {
		case 1:
			if err := p.ReadField1(iprot); err != nil {
				return err
			}
		default:
			if err := iprot.Skip(fieldTypeId); err != nil {
				return err
			}
		}
		if err := iprot.ReadFieldEnd(); err != nil {
			return err
		}
	}
	if err := iprot.ReadStructEnd(); err != nil {
		return fmt.Errorf("%T read struct end error: %s", p, err)
	}
	return nil
}

func (p *OpenPublisherStreamArgs) ReadField1(iprot thrift.TProtocol) error {
	p.Messages = &PutMessageStream{}
	if err := p.Messages.Read(iprot); err != nil {
		return fmt.Errorf("%T error reading struct: %s", p.Messages, err)
	}
	return nil
}

func (p *OpenPublisherStreamArgs) Write(oprot thrift.TProtocol) error {
	if err := oprot.WriteStructBegin("OpenPublisherStream_args"); err != nil {
		return fmt.Errorf("%T write struct begin error: %s", p, err)
	}
	if err := p.writeField1(oprot); err != nil {
		return err
	}
	if err := oprot.WriteFieldStop(); err != nil {
		return fmt.Errorf("write field stop error: %s", err)
	}
	if err := oprot.WriteStructEnd(); err != nil {
		return fmt.Errorf("write struct stop error: %s", err)
	}
	return nil
}

func (p *OpenPublisherStreamArgs) writeField1(oprot thrift.TProtocol) (err error) {
	if err := oprot.WriteFieldBegin("messages", thrift.STRUCT, 1); err != nil {
		return fmt.Errorf("%T write field begin error 1:messages: %s", p, err)
	}
	if err := p.Messages.Write(oprot); err != nil {
		return fmt.Errorf("%T error writing struct: %s", p.Messages, err)
	}
	if err := oprot.WriteFieldEnd(); err != nil {
		return fmt.Errorf("%T write field end error 1:messages: %s", p, err)
	}
	return err
}

func (p *OpenPublisherStreamArgs) String() string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("OpenPublisherStreamArgs(%+v)", *p)
}

type OpenPublisherStreamResult struct {
	Success        *PutMessageAckStream  `thrift:"success,0" json:"success"`
	EntityError    *EntityNotExistsError `thrift:"entityError,1" json:"entityError"`
	EntityDisabled *EntityDisabledError  `thrift:"entityDisabled,2" json:"entityDisabled"`
	RequestError   *BadRequestError      `thrift:"requestError,3" json:"requestError"`
}

func NewOpenPublisherStreamResult() *OpenPublisherStreamResult {
	return &OpenPublisherStreamResult{}
}

var OpenPublisherStreamResult_Success_DEFAULT *PutMessageAckStream

func (p *OpenPublisherStreamResult) GetSuccess() *PutMessageAckStream {
	if !p.IsSetSuccess() {
		return OpenPublisherStreamResult_Success_DEFAULT
	}
	return p.Success
}

var OpenPublisherStreamResult_EntityError_DEFAULT *EntityNotExistsError

func (p *OpenPublisherStreamResult) GetEntityError() *EntityNotExistsError {
	if !p.IsSetEntityError() {
		return OpenPublisherStreamResult_EntityError_DEFAULT
	}
	return p.EntityError
}

var OpenPublisherStreamResult_EntityDisabled_DEFAULT *EntityDisabledError

func (p *OpenPublisherStreamResult) GetEntityDisabled() *EntityDisabledError {
	if !p.IsSetEntityDisabled() {
		return OpenPublisherStreamResult_EntityDisabled_DEFAULT
	}
	return p.EntityDisabled
}

var OpenPublisherStreamResult_RequestError_DEFAULT *BadRequestError

func (p *OpenPublisherStreamResult) GetRequestError() *BadRequestError {
	if !p.IsSetRequestError() {
		return OpenPublisherStreamResult_RequestError_DEFAULT
	}
	return p.RequestError
}
func (p *OpenPublisherStreamResult) IsSetSuccess() bool {
	return p.Success != nil
}

func (p *OpenPublisherStreamResult) IsSetEntityError() bool {
	return p.EntityError != nil
}

func (p *OpenPublisherStreamResult) IsSetEntityDisabled() bool {
	return p.EntityDisabled != nil
}

func (p *OpenPublisherStreamResult) IsSetRequestError() bool {
	return p.RequestError != nil
}

func (p *OpenPublisherStreamResult) Read(iprot thrift.TProtocol) error {
	if _, err := iprot.ReadStructBegin(); err != nil {
		return fmt.Errorf("%T read error: %s", p, err)
	}
	for {
		_, fieldTypeId, fieldId, err := iprot.ReadFieldBegin()
		if err != nil {
			return fmt.Errorf("%T field %d read error: %s", p, fieldId, err)
		}
		if fieldTypeId == thrift.STOP {
			break
		}
		switch fieldId {
		case 0:
			if err := p.ReadField0(iprot); err != nil {
				return err
			}
		case 1:
			if err := p.ReadField1(iprot); err != nil {
				return err
			}
		case 2:
			if err := p.ReadField2(iprot); err != nil {
				return err
			}
		case 3:
			if err := p.ReadField3(iprot); err != nil {
				return err
			}
		default:
			if err := iprot.Skip(fieldTypeId); err != nil {
				return err
			}
		}
		if err := iprot.ReadFieldEnd(); err != nil {
			return err
		}
	}
	if err := iprot.ReadStructEnd(); err != nil {
		return fmt.Errorf("%T read struct end error: %s", p, err)
	}
	return nil
}

func (p *OpenPublisherStreamResult) ReadField0(iprot thrift.TProtocol) error {
	p.Success = &PutMessageAckStream{}
	if err := p.Success.Read(iprot); err != nil {
		return fmt.Errorf("%T error reading struct: %s", p.Success, err)
	}
	return nil
}

func (p *OpenPublisherStreamResult) ReadField1(iprot thrift.TProtocol) error {
	p.EntityError = &EntityNotExistsError{}
	if err := p.EntityError.Read(iprot); err != nil {
		return fmt.Errorf("%T error reading struct: %s", p.EntityError, err)
	}
	return nil
}

func (p *OpenPublisherStreamResult) ReadField2(iprot thrift.TProtocol) error {
	p.EntityDisabled = &EntityDisabledError{}
	if err := p.EntityDisabled.Read(iprot); err != nil {
		return fmt.Errorf("%T error reading struct: %s", p.EntityDisabled, err)
	}
	return nil
}

func (p *OpenPublisherStreamResult) ReadField3(iprot thrift.TProtocol) error {
	p.RequestError = &BadRequestError{}
	if err := p.RequestError.Read(iprot); err != nil {
		return fmt.Errorf("%T error reading struct: %s", p.RequestError, err)
	}
	return nil
}

func (p *OpenPublisherStreamResult) Write(oprot thrift.TProtocol) error {
	if err := oprot.WriteStructBegin("OpenPublisherStream_result"); err != nil {
		return fmt.Errorf("%T write struct begin error: %s", p, err)
	}
	if err := p.writeField0(oprot); err != nil {
		return err
	}
	if err := p.writeField1(oprot); err != nil {
		return err
	}
	if err := p.writeField2(oprot); err != nil {
		return err
	}
	if err := p.writeField3(oprot); err != nil {
		return err
	}
	if err := oprot.WriteFieldStop(); err != nil {
		return fmt.Errorf("write field stop error: %s", err)
	}
	if err := oprot.WriteStructEnd(); err != nil {
		return fmt.Errorf("write struct stop error: %s", err)
	}
	return nil
}

func (p *OpenPublisherStreamResult) writeField0(oprot thrift.TProtocol) (err error) {
	if p.IsSetSuccess() {
		if err := oprot.WriteFieldBegin("success", thrift.STRUCT, 0); err != nil {
			return fmt.Errorf("%T write field begin error 0:success: %s", p, err)
		}
		if err := p.Success.Write(oprot); err != nil {
			return fmt.Errorf("%T error writing struct: %s", p.Success, err)
		}
		if err := oprot.WriteFieldEnd(); err != nil {
			return fmt.Errorf("%T write field end error 0:success: %s", p, err)
		}
	}
	return err
}

func (p *OpenPublisherStreamResult) writeField1(oprot thrift.TProtocol) (err error) {
	if p.IsSetEntityError() {
		if err := oprot.WriteFieldBegin("entityError", thrift.STRUCT, 1); err != nil {
			return fmt.Errorf("%T write field begin error 1:entityError: %s", p, err)
		}
		if err := p.EntityError.Write(oprot); err != nil {
			return fmt.Errorf("%T error writing struct: %s", p.EntityError, err)
		}
		if err := oprot.WriteFieldEnd(); err != nil {
			return fmt.Errorf("%T write field end error 1:entityError: %s", p, err)
		}
	}
	return err
}

func (p *OpenPublisherStreamResult) writeField2(oprot thrift.TProtocol) (err error) {
	if p.IsSetEntityDisabled() {
		if err := oprot.WriteFieldBegin("entityDisabled", thrift.STRUCT, 2); err != nil {
			return fmt.Errorf("%T write field begin error 2:entityDisabled: %s", p, err)
		}
		if err := p.EntityDisabled.Write(oprot); err != nil {
			return fmt.Errorf("%T error writing struct: %s", p.EntityDisabled, err)
		}
		if err := oprot.WriteFieldEnd(); err != nil {
			return fmt.Errorf("%T write field end error 2:entityDisabled: %s", p, err)
		}
	}
	return err
}

func (p *OpenPublisherStreamResult) writeField3(oprot thrift.TProtocol) (err error) {
	if p.IsSetRequestError() {
		if err := oprot.WriteFieldBegin("requestError", thrift.STRUCT, 3); err != nil {
			return fmt.Errorf("%T write field begin error 3:requestError: %s", p, err)
		}
		if err := p.RequestError.Write(oprot); err != nil {
			return fmt.Errorf("%T error writing struct: %s", p.RequestError, err)
		}
		if err := oprot.WriteFieldEnd(); err != nil {
			return fmt.Errorf("%T write field end error 3:requestError: %s", p, err)
		}
	}
	return err
}

func (p *OpenPublisherStreamResult) String() string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("OpenPublisherStreamResult(%+v)", *p)
}
