```
[xaionaro@void unhash]$ ls -ldh ~/firmware/samplehost/firmware.fd
-r--r----- 1 xaionaro xaionaro 16M Apr  5 15:33 /home/xaionaro/firmware/samplehost/firmware.fd
[xaionaro@void unhash]$ time go run ./cmd/findhashinbinary/ sha256 FB4DA84CADAB0FF6ABD9AB6354D850A472C2F245D6EFA8DB6D4B9FE47D525EE3 ~/firmware/samplehost/firmware.fd
Checked  31075062  hash values

Found!

start_pos = 5648452 (0x563044)
  end_pos = 5648484 (0x563064)

the data to be hashed is: 500E22B42E2732F6CB7B626D3491F3650440BBC47390B5A07CB2168D70F42078

real	0m1.566s
user	0m21.280s
sys     0m0.150s
[xaionaro@void unhash]$ echo 500E22B42E2732F6CB7B626D3491F3650440BBC47390B5A07CB2168D70F42078 | xxd -r -p | sha256sum
fb4da84cadab0ff6abd9ab6354d850a472c2f245d6efa8db6d4b9fe47d525ee3  -
```

Larger chunk:
```
[xaionaro@void unhash]$ go build -o /tmp/b ./cmd/findhashinbinary/&& time /tmp/b sha256 A80498A387A12531226FD9D86CF50797AB9DA9EBA6CECB1E6707FF5EB863F3D6 ~/firmware/samplehost2/firmware.fd
Checked  189819343  hash values

Found!

start_pos = 1114112 (0x110000)
  end_pos = 7073792 (0x6BF000)

real	0m8.118s
user	2m3.601s
sys     0m0.123s
```
