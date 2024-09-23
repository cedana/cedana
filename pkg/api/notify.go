package api

import "github.com/rs/zerolog/log"

type Notify struct {
	PreDumpFunc    NotifyFunc
	PostDumpFunc   NotifyFunc
	PreRestoreFunc NotifyFunc
	PreResumeFunc  NotifyFunc
}

type NotifyFunc struct {
	Avail    bool
	Callback func() error
}

func (n Notify) PreDump() error {
	if n.PreDumpFunc.Avail {
		log.Debug().Msgf("executing predump anonymous function")
		err := n.PreDumpFunc.Callback()
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute predump anonymous function")
			return err
		}
	}
	return nil
}

func (n Notify) PostDump() error {
	if n.PostDumpFunc.Avail {
		log.Debug().Msgf("executing postdump anonymous function")
		err := n.PostDumpFunc.Callback()
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute postdump anonymous function")
			return err
		}
	}
	return nil
}

func (n Notify) PreRestore() error {
	if n.PreRestoreFunc.Avail {
		log.Debug().Msgf("executing prerestore anonymous function")
		err := n.PreRestoreFunc.Callback()
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute prerestore anonymous function")
			return err
		}
	}
	return nil
}

func (n Notify) PreResume() error {
	if n.PreResumeFunc.Avail {
		log.Debug().Msgf("executing prerestore anonymous function")
		err := n.PreResumeFunc.Callback()
		if err != nil {
			log.Error().Err(err).Msgf("failed to execute prerestore anonymous function")
			return err
		}
	}
	return nil
}

// PostRestore NoNotify
func (n Notify) PostRestore(pid int32) error {
	return nil
}

// NetworkLock NoNotify
func (n Notify) NetworkLock() error {
	return nil
}

// NetworkUnlock NoNotify
func (n Notify) NetworkUnlock() error {
	return nil
}

// SetupNamespaces NoNotify
func (n Notify) SetupNamespaces(pid int32) error {
	return nil
}

// PostSetupNamespaces NoNotify
func (n Notify) PostSetupNamespaces() error {
	return nil
}

// PostResume NoNotify
func (n Notify) PostResume() error {
	return nil
}
