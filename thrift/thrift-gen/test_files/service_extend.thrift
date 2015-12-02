service S1 {
  void M1()
}

service S2 extends S1 {
  void M2()
}

service S3 extends S2 {
  void M3()
}

//Go code: service_extend/test.go
// package service_extend
// var _ = TChanS3(nil).M1
// var _ = TChanS3(nil).M2
// var _ = TChanS3(nil).M3
