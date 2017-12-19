package account_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	gethcommon "github.com/ethereum/go-ethereum/common"
	whisper "github.com/ethereum/go-ethereum/whisper/whisperv5"
	"github.com/golang/mock/gomock"
	"github.com/status-im/status-go/geth/account"
	"github.com/status-im/status-go/geth/common"
	. "github.com/status-im/status-go/testing"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestVerifyAccountPassword(t *testing.T) {
	acctManager := account.NewManager(nil)
	keyStoreDir, err := ioutil.TempDir(os.TempDir(), "accounts")
	require.NoError(t, err)
	defer os.RemoveAll(keyStoreDir) //nolint: errcheck

	emptyKeyStoreDir, err := ioutil.TempDir(os.TempDir(), "accounts_empty")
	require.NoError(t, err)
	defer os.RemoveAll(emptyKeyStoreDir) //nolint: errcheck

	// import account keys
	require.NoError(t, common.ImportTestAccount(keyStoreDir, GetAccount1PKFile()))
	require.NoError(t, common.ImportTestAccount(keyStoreDir, GetAccount2PKFile()))

	account1Address := gethcommon.BytesToAddress(gethcommon.FromHex(TestConfig.Account1.Address))

	testCases := []struct {
		name          string
		keyPath       string
		address       string
		password      string
		expectedError error
	}{
		{
			"correct address, correct password (decrypt should succeed)",
			keyStoreDir,
			TestConfig.Account1.Address,
			TestConfig.Account1.Password,
			nil,
		},
		{
			"correct address, correct password, non-existent key store",
			filepath.Join(keyStoreDir, "non-existent-folder"),
			TestConfig.Account1.Address,
			TestConfig.Account1.Password,
			fmt.Errorf("cannot traverse key store folder: lstat %s/non-existent-folder: no such file or directory", keyStoreDir),
		},
		{
			"correct address, correct password, empty key store (pk is not there)",
			emptyKeyStoreDir,
			TestConfig.Account1.Address,
			TestConfig.Account1.Password,
			fmt.Errorf("cannot locate account for address: %s", account1Address.Hex()),
		},
		{
			"wrong address, correct password",
			keyStoreDir,
			"0x79791d3e8f2daa1f7fec29649d152c0ada3cc535",
			TestConfig.Account1.Password,
			fmt.Errorf("cannot locate account for address: %s", "0x79791d3E8F2dAa1F7FeC29649d152c0aDA3cc535"),
		},
		{
			"correct address, wrong password",
			keyStoreDir,
			TestConfig.Account1.Address,
			"wrong password", // wrong password
			errors.New("could not decrypt key with given passphrase"),
		},
	}
	for _, testCase := range testCases {
		accountKey, err := acctManager.VerifyAccountPassword(testCase.keyPath, testCase.address, testCase.password)
		if !reflect.DeepEqual(err, testCase.expectedError) {
			require.FailNow(t, fmt.Sprintf("unexpected error: expected \n'%v', got \n'%v'", testCase.expectedError, err))
		}
		if err == nil {
			if accountKey == nil {
				require.Fail(t, "no error reported, but account key is missing")
			}
			accountAddress := gethcommon.BytesToAddress(gethcommon.FromHex(testCase.address))
			if accountKey.Address != accountAddress {
				require.Fail(t, "account mismatch: have %s, want %s", accountKey.Address.Hex(), accountAddress.Hex())
			}
		}
	}
}

// TestVerifyAccountPasswordWithAccountBeforeEIP55 verifies if VerifyAccountPassword
// can handle accounts before introduction of EIP55.
func TestVerifyAccountPasswordWithAccountBeforeEIP55(t *testing.T) {
	keyStoreDir, err := ioutil.TempDir("", "status-accounts-test")
	require.NoError(t, err)
	defer os.RemoveAll(keyStoreDir) //nolint: errcheck

	// Import keys and make sure one was created before EIP55 introduction.
	err = common.ImportTestAccount(keyStoreDir, "test-account3-before-eip55.pk")
	require.NoError(t, err)

	acctManager := account.NewManager(nil)

	address := gethcommon.HexToAddress(TestConfig.Account3.Address)
	_, err = acctManager.VerifyAccountPassword(keyStoreDir, address.Hex(), TestConfig.Account3.Password)
	require.NoError(t, err)
}

func TestManagerTestSuite(t *testing.T) {
	ctrl := gomock.NewController(t)
	nodeManager := common.NewMockNodeManager(ctrl)

	keyStoreDir, err := ioutil.TempDir(os.TempDir(), "accounts")
	require.NoError(t, err)
	keyStore := keystore.NewKeyStore(keyStoreDir, keystore.LightScryptN, keystore.LightScryptP)
	defer os.RemoveAll(keyStoreDir) //nolint: errcheck

	suite.Run(t, &ManagerTestSuite{
		nodeManager: nodeManager,
		accManager:  account.NewManager(nodeManager),
		password:    "test-password",
		keyStore:    keyStore,
		shh:         whisper.New(nil),
	})
}

type ManagerTestSuite struct {
	suite.Suite
	nodeManager *common.MockNodeManager
	accManager  *account.Manager
	password    string
	keyStore    *keystore.KeyStore
	shh         *whisper.Whisper
}

func (s *ManagerTestSuite) TestCreateAndRecoverAccountSuccess() {
	accManager, nodeManager, password, keyStore := s.accManager, s.nodeManager, s.password, s.keyStore

	// Don't fail on empty password
	nodeManager.EXPECT().AccountKeyStore().Return(keyStore, nil)
	_, _, _, err := accManager.CreateAccount(password)
	s.NoError(err)

	password = s.password

	nodeManager.EXPECT().AccountKeyStore().Return(keyStore, nil)
	addr1, pubKey1, mnemonic, err := accManager.CreateAccount(password)
	s.NoError(err)
	s.NotNil(addr1)
	s.NotNil(pubKey1)
	s.NotNil(mnemonic)

	// Now recover the account using the mnemonic seed and the password
	nodeManager.EXPECT().AccountKeyStore().Return(keyStore, nil)
	addr2, pubKey2, err := accManager.RecoverAccount(password, mnemonic)
	s.NoError(err)
	s.Equal(addr1, addr2)
	s.Equal(pubKey1, pubKey2)
}

func (s *ManagerTestSuite) TestCreateAndRecoverAccountFail_KeyStore() {
	accManager, nodeManager, password, keyStore := s.accManager, s.nodeManager, s.password, s.keyStore

	expectedErr := errors.New("Non-nil error string")
	nodeManager.EXPECT().AccountKeyStore().Return(nil, expectedErr)
	_, _, _, err := accManager.CreateAccount(password)
	s.Equal(err, expectedErr)

	// Create a new account to use the mnemonic seed.
	nodeManager.EXPECT().AccountKeyStore().Return(keyStore, nil)
	_, _, mnemonic, err := accManager.CreateAccount(password)
	s.NoError(err)

	nodeManager.EXPECT().AccountKeyStore().Return(nil, expectedErr)
	_, _, err = accManager.RecoverAccount(password, mnemonic)
	s.Equal(err, expectedErr)
}

func (s *ManagerTestSuite) TestSelectAccount() {
	accManager, nodeManager, password, keyStore := s.accManager, s.nodeManager, s.password, s.keyStore
	shh := s.shh

	nodeManager.EXPECT().AccountKeyStore().Return(keyStore, nil)
	addr, _, _, err := accManager.CreateAccount(password)
	s.NoError(err)

	testCases := []struct {
		name                  string
		accountKeyStoreReturn []interface{}
		whisperServiceReturn  []interface{}
		address               string
		password              string
		fail                  bool
	}{
		{
			"success",
			[]interface{}{keyStore, nil},
			[]interface{}{shh, nil},
			addr,
			password,
			false,
		},
		{
			"fail_keyStore",
			[]interface{}{nil, errors.New("Can't return you a key store")},
			[]interface{}{shh, nil},
			addr,
			password,
			true,
		},
		{
			"fail_whisperService",
			[]interface{}{keyStore, nil},
			[]interface{}{nil, errors.New("Can't return you a whisper service")},
			addr,
			password,
			true,
		},
		{
			"fail_wrongAddress",
			[]interface{}{keyStore, nil},
			[]interface{}{shh, nil},
			"wrong-address",
			password,
			true,
		},
		{
			"fail_wrongPassword",
			[]interface{}{keyStore, nil},
			[]interface{}{shh, nil},
			addr,
			"wrong-password",
			true,
		},
	}

	for _, testCase := range testCases {
		s.T().Run(testCase.name, func(t *testing.T) {
			nodeManager.EXPECT().AccountKeyStore().Return(testCase.accountKeyStoreReturn...).Times(2)
			nodeManager.EXPECT().WhisperService().Return(testCase.whisperServiceReturn...)
			err = accManager.SelectAccount(testCase.address, testCase.password)
			if testCase.fail {
				s.Error(err)
			} else {
				s.NoError(err)
			}
		})
	}
}
