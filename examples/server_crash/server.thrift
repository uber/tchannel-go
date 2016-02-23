exception EntityNotExistsError {
  1: string message
}

exception EntityDisabledError {
  1: string message
}

exception BadRequestError {
  1: string message
}

enum Status {
  OK,
  FAILED,
  TIMEDOUT
}

struct PutMessage {
  1: string id
  2: i32 delayMessageInSeconds
  3: string data
}

struct PutMessageStream {}

struct PutMessageAck {
  1: string id
  2: Status status
  3: string message  // This is optional
}

struct PutMessageAckStream {}

service BIn {
  PutMessageAckStream OpenPublisherStream(
    //1: string path,  // TODO: this is not supported
    1: PutMessageStream messages)
      throws (
        1: EntityNotExistsError entityError,
        2: EntityDisabledError entityDisabled,
        3: BadRequestError requestError)
}

