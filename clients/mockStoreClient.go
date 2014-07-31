package clients

import (
	"crypto/rand"
	"errors"
	"github.com/tidepool-org/shoreline/models"
)

type MockStoreClient struct {
	salt  string
	doBad bool
}

func NewMockStoreClient(salt string, doBad bool) *MockStoreClient {
	return &MockStoreClient{salt: salt, doBad: doBad}
}

//faking the hashes
func rand_str(str_size int) string {
	alphanum := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var bytes = make([]byte, str_size)
	rand.Read(bytes)
	for i, b := range bytes {
		bytes[i] = alphanum[b%byte(len(alphanum))]
	}
	return string(bytes)
}

func (d MockStoreClient) Close() {}

func (d MockStoreClient) UpsertUser(user *models.User) error {
	if d.doBad {
		return errors.New("UpsertUser failure")
	}
	return nil
}

func (d MockStoreClient) FindUsers(user *models.User) (found []*models.User, err error) {
	//`find` a pretend one we just made

	if d.doBad {
		return found, errors.New("FindUsers failure")
	}

	if user.Id == "" && user.Pw != "" && user.Name != "" {
		found, _ := models.NewUser(&models.UserDetail{Name: user.Name, Pw: user.Pw, Emails: []string{}}, d.salt)
		return []*models.User{found}, nil
	}

	return []*models.User{user}, nil
}

func (d MockStoreClient) FindUser(user *models.User) (found *models.User, err error) {

	if d.doBad {
		return found, errors.New("FindUser failure")
	}
	//`find` a pretend one we just made
	if user.Id == "" && user.Pw != "" && user.Name != "" {
		found, _ := models.NewUser(&models.UserDetail{Name: user.Name, Pw: user.Pw, Emails: []string{}}, d.salt)
		return found, nil
	}
	return user, nil
}

func (d MockStoreClient) RemoveUser(user *models.User) error {
	if d.doBad {
		return errors.New("RemoveUser failure")
	}
	return nil
}

func (d MockStoreClient) AddToken(token *models.SessionToken) error {
	if d.doBad {
		return errors.New("AddToken failure")
	}
	return nil
}

func (d MockStoreClient) FindToken(token *models.SessionToken) (*models.SessionToken, error) {
	if d.doBad {
		return token, errors.New("FindToken failure")
	}
	//`find` a pretend one we just made
	return token, nil
}

func (d MockStoreClient) RemoveToken(token *models.SessionToken) error {
	if d.doBad {
		return errors.New("RemoveToken failure")
	}
	return nil
}
